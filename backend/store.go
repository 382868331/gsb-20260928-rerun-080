package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store is an append-only, manually fed block index. It rebuilds chain
// state from a local JSONL log; it never dials a public node.
type Store struct {
	mu       sync.RWMutex
	dir      string
	blocks   map[string]*Block
	order    []string
	versions map[int64]*Snapshot
	current  int64
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, blocks: map[string]*Block{}, versions: map[int64]*Snapshot{}}
	if err := s.replay(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) logPath() string { return filepath.Join(s.dir, "blocks.log") }
func (s *Store) tmpPath() string { return filepath.Join(s.dir, "blocks.log.tmp") }

// replay rebuilds accepted blocks and every committed snapshot version.
func (s *Store) replay() error {
	f, err := os.Open(s.logPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	var blocks []Block
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var b Block
		if err := json.Unmarshal(line, &b); err != nil {
			return fmt.Errorf("corrupt block log: %w", err)
		}
		blocks = append(blocks, b)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	for i := range blocks {
		b := blocks[i]
		if _, exists := s.blocks[b.Hash]; exists {
			continue // interrupted import must not index a block twice
		}
		s.blocks[b.Hash] = &b
		s.order = append(s.order, b.Hash)
		s.rebuildLocked(true)
	}
	return nil
}

// appendBlocks durably appends blocks before reindexing. The append and the
// state swap are not a single transaction, so append uses a temp file rename
// per import: readers always see a complete log, and restart replays it.
func (s *Store) appendBlocks(newBlocks []Block) error {
	return s.appendBlocksLocked(newBlocks)
}

// appendBlocksLocked assumes s.mu is held for writing.
func (s *Store) appendBlocksLocked(newBlocks []Block) error {
	all := make([]Block, 0, len(s.order)+len(newBlocks))
	seen := map[string]bool{}
	for _, h := range s.order {
		all = append(all, *s.blocks[h])
		seen[h] = true
	}
	for _, b := range newBlocks {
		if !seen[b.Hash] {
			all = append(all, b)
			seen[b.Hash] = true
		}
	}

	f, err := os.OpenFile(s.tmpPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(f)
	enc := json.NewEncoder(bw)
	for i := range all {
		if err := enc.Encode(&all[i]); err != nil {
			f.Close()
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(s.tmpPath(), s.logPath())
}

// Import validates, deduplicates and indexes a batch atomically.
func (s *Store) Import(in []Block) ImportResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev := s.current
	res := ImportResult{PreviousVersion: prev, CurrentVersion: prev}
	var fresh []Block
	batchSeen := map[string]bool{}

	for i := range in {
		b := in[i]
		if b.Hash == "" {
			res.Invalid = append(res.Invalid, fmt.Sprintf("#%d: missing hash", i))
			continue
		}
		if s.blocks[b.Hash] != nil {
			res.Skipped = append(res.Skipped, b.Hash)
			continue
		}
		if batchSeen[b.Hash] {
			res.Skipped = append(res.Skipped, b.Hash)
			continue
		}
		if err := validateBlock(&b); err != nil {
			res.Invalid = append(res.Invalid, fmt.Sprintf("%s: %v", b.Hash, err))
			continue
		}
		batchSeen[b.Hash] = true
		fresh = append(fresh, b)
	}
	if len(fresh) == 0 {
		if cur := s.versions[s.current]; cur != nil {
			res.CurrentVersion = s.current
			res.TipHash = cur.TipHash
		}
		return res
	}
	if err := s.appendBlocksLocked(fresh); err != nil {
		res.Invalid = append(res.Invalid, "persistence failed: "+err.Error())
		return res
	}
	now := time.Now().UTC()
	for i := range fresh {
		b := fresh[i]
		b.ReceivedAt = now
		s.blocks[b.Hash] = &b
		s.order = append(s.order, b.Hash)
		res.Imported = append(res.Imported, b.Hash)
	}
	s.rebuildLocked(false)
	cur := s.versions[s.current]
	res.CurrentVersion = s.current
	res.TipHash = cur.TipHash
	res.Reason = cur.Reason
	res.Changed = s.current != prev
	return res
}

func validateBlock(b *Block) error {
	if b.ParentHash == "" && b.Height != 0 {
		return fmt.Errorf("non-genesis block needs parentHash")
	}
	if b.Height < 0 {
		return fmt.Errorf("negative height")
	}
	txSeen := map[string]bool{}
	for _, tx := range b.Txs {
		if tx.Hash == "" {
			return fmt.Errorf("transaction missing hash")
		}
		if txSeen[tx.Hash] {
			return fmt.Errorf("duplicate transaction %s inside block", tx.Hash)
		}
		txSeen[tx.Hash] = true
	}
	return nil
}

// Current returns the latest snapshot.
func (s *Store) Current() *Snapshot { return s.Version(0) }

// Version returns v, or the current version when v == 0.
func (s *Store) Version(v int64) *Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v == 0 {
		v = s.current
	}
	return s.versions[v]
}

func (s *Store) Versions() []int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]int64, 0, len(s.versions))
	for v := range s.versions {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
