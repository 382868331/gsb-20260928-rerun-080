package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"chainresearch/internal/model"
)

// Store 保存所有已导入的原始区块与历史版本快照。
// 区块按 Hash 去重，导入中断后重放同一批数据不会产生重复索引。
type Store struct {
	mu            sync.RWMutex
	path          string
	blocks        map[string]model.Block
	snapshots     []model.VersionSnapshot
	checkpoint    *model.Checkpoint
	researchLines []model.ResearchLine
}

type persisted struct {
	Blocks        []model.Block           `json:"blocks"`
	Snapshots     []model.VersionSnapshot `json:"snapshots"`
	Checkpoint    *model.Checkpoint       `json:"checkpoint,omitempty"`
	ResearchLines []model.ResearchLine    `json:"research_lines,omitempty"`
}

func Open(dir string) (*Store, error) {
	s := &Store{
		path:   filepath.Join(dir, "store.json"),
		blocks: map[string]model.Block{},
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path)
	if err == nil {
		var p persisted
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, fmt.Errorf("损坏的索引文件: %w", err)
		}
		for _, b := range p.Blocks {
			s.blocks[b.Hash] = b
		}
		s.snapshots = p.Snapshots
		s.checkpoint = p.Checkpoint
		s.researchLines = p.ResearchLines
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

// SetCheckpoint 保存研究者确认的检查点（含确认依据），并落盘。
func (s *Store) SetCheckpoint(cp *model.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoint = cp
	return s.flushLocked()
}

// ClearCheckpoint 由研究者显式撤销检查点确认。
func (s *Store) ClearCheckpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoint = nil
	return s.flushLocked()
}

// Checkpoint 返回当前检查点（未确认为 nil）。
func (s *Store) Checkpoint() *model.Checkpoint {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.checkpoint
}

// AddResearchLine 登记一条独立研究线（按端点 Hash 幂等）。
func (s *Store) AddResearchLine(line model.ResearchLine) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rl := range s.researchLines {
		if rl.TipHash == line.TipHash {
			return nil
		}
	}
	s.researchLines = append(s.researchLines, line)
	return s.flushLocked()
}

// ResearchLines 返回已登记的全部独立研究线。
func (s *Store) ResearchLines() []model.ResearchLine {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ResearchLine, len(s.researchLines))
	copy(out, s.researchLines)
	return out
}

// ImportResult 报告本次导入的实际变化。
type ImportResult struct {
	Added   int
	Skipped int
}

// AddBlocks 幂等导入：已存在且内容一致的区块跳过；
// 同 Hash 不同内容视为冲突并拒绝（事务性：全部不写入）。
func (s *Store) AddBlocks(incoming []model.Block) (ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := ImportResult{}
	// 先校验整批，保证失败时不留下部分写入。
	seen := map[string]bool{}
	for _, b := range incoming {
		if b.Hash == "" {
			return result, fmt.Errorf("区块缺少 hash")
		}
		if seen[b.Hash] {
			return result, fmt.Errorf("同批次内区块重复: %s", b.Hash)
		}
		seen[b.Hash] = true
		if existing, ok := s.blocks[b.Hash]; ok {
			if blockDigest(existing) != blockDigest(b) {
				return result, fmt.Errorf("区块 %s 内容与已索引区块冲突", b.Hash)
			}
		}
	}
	for _, b := range incoming {
		if _, ok := s.blocks[b.Hash]; ok {
			result.Skipped++
			continue
		}
		s.blocks[b.Hash] = b
		result.Added++
	}
	if result.Added > 0 {
		if err := s.flushLocked(); err != nil {
			return result, err
		}
	}
	return result, nil
}

// SaveSnapshot 追加一个新版本快照并落盘。
func (s *Store) SaveSnapshot(snap model.VersionSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = append(s.snapshots, snap)
	return s.flushLocked()
}

func (s *Store) Snapshot(version int64) (model.VersionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, snap := range s.snapshots {
		if snap.Version == version {
			return snap, true
		}
	}
	return model.VersionSnapshot{}, false
}

func (s *Store) LatestSnapshot() (model.VersionSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.snapshots) == 0 {
		return model.VersionSnapshot{}, false
	}
	return s.snapshots[len(s.snapshots)-1], true
}

func (s *Store) Versions() []model.VersionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.VersionSnapshot, len(s.snapshots))
	copy(out, s.snapshots)
	return out
}

func (s *Store) AllBlocks() []model.Block {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Block, 0, len(s.blocks))
	for _, b := range s.blocks {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hash < out[j].Hash })
	return out
}

func (s *Store) flushLocked() error {
	blocks := make([]model.Block, 0, len(s.blocks))
	for _, b := range s.blocks {
		blocks = append(blocks, b)
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Hash < blocks[j].Hash })
	p := persisted{
		Blocks:        blocks,
		Snapshots:     s.snapshots,
		Checkpoint:    s.checkpoint,
		ResearchLines: s.researchLines,
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// blockDigest 对区块全部业务内容取摘要，用于检测同 Hash 冲突。
func blockDigest(b model.Block) string {
	data, _ := json.Marshal(b)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
