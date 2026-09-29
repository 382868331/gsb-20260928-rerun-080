package main

import (
	"sort"
	"time"
)

const keepVersions = 64

// rebuildLocked recomputes canonical chain, transaction positions and a new
// immutable snapshot. Caller must hold s.mu.
func (s *Store) rebuildLocked(replay bool) {
	prevTip := ""
	if cur := s.versions[s.current]; cur != nil {
		prevTip = cur.TipHash
	}

	children := map[string][]string{}
	for h, b := range s.blocks {
		if b.ParentHash == "" {
		} else if _, ok := s.blocks[b.ParentHash]; ok {
			children[b.ParentHash] = append(children[b.ParentHash], h)
		}
	}
	for h := range children {
		sort.Strings(children[h])
	}

	// Derive height and cumulative work along connected chains.
	height := map[string]int64{}
	chainWork := map[string]uint64{}
	orphan := map[string]bool{}

	var visit func(h string, trail map[string]bool) (connected bool)
	visit = func(h string, trail map[string]bool) bool {
		b, ok := s.blocks[h]
		if !ok {
			return false
		}
		if trail[h] {
			return false // parent cycle: cannot reach a genesis
		}
		if _, done := height[h]; done {
			return !orphan[h]
		}
		if b.ParentHash == "" {
			height[h] = 0
			w := b.Weight
			if w == 0 {
				w = 1
			}
			chainWork[h] = w
			return true
		}
		if _, parentKnown := s.blocks[b.ParentHash]; !parentKnown {
			orphan[h] = true
			return false
		}
		trail[h] = true
		if !visit(b.ParentHash, trail) {
			delete(trail, h)
			orphan[h] = true
			return false
		}
		delete(trail, h)
		height[h] = height[b.ParentHash] + 1
		w := b.Weight
		if w == 0 {
			w = 1
		}
		chainWork[h] = saturatingAdd(chainWork[b.ParentHash], w)
		return true
	}
	for h := range s.blocks {
		visit(h, map[string]bool{})
	}

	// Pick the best tip: most cumulative work, then greatest height, then
	// smallest hash for deterministic tie-breaking.
	bestTip := ""
	for h := range s.blocks {
		if orphan[h] || len(children[h]) > 0 {
			continue
		}
		if bestTip == "" || betterTip(h, bestTip, chainWork, height) {
			bestTip = h
		}
	}
	canonical := map[string]bool{}
	if bestTip != "" {
		h := bestTip
		for h != "" {
			canonical[h] = true
			b := s.blocks[h]
			if b.ParentHash == "" || !s.hasBlock(b.ParentHash) {
				break
			}
			h = b.ParentHash
		}
	}

	tipHeight := int64(-1)
	if bestTip != "" {
		tipHeight = height[bestTip]
	}

	snap := &Snapshot{
		PreviousVersion: s.current,
		CreatedAt:       time.Now().UTC(),
		TipHash:         bestTip,
		Height:          tipHeight,
		blockIndex:      map[string]*BlockView{},
		txIndex:         map[string]*TransactionView{},
	}

	// Stable block order: canonical chain first by height, then forks, then orphans.
	hashes := make([]string, 0, len(s.blocks))
	for h := range s.blocks {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool {
		a, b := hashes[i], hashes[j]
		ac, bc := canonical[a], canonical[b]
		if ac != bc {
			return ac
		}
		ao, bo := orphan[a], orphan[b]
		if ao != bo {
			return !ao
		}
		if height[a] != height[b] {
			return height[a] < height[b]
		}
		return a < b
	})

	for _, h := range hashes {
		b := s.blocks[h]
		status := StatusForked
		if canonical[h] {
			status = StatusCanonical
		} else if orphan[h] {
			status = StatusOrphan
		}
		var conf int64
		cw := chainWork[h]
		if orphan[h] {
			cw = 0
		}
		if canonical[h] {
			conf = tipHeight - height[h] + 1
		}
		txHashes := make([]string, len(b.Txs))
		for i, tx := range b.Txs {
			txHashes[i] = tx.Hash
		}
		bv := BlockView{
			Hash: h, ParentHash: b.ParentHash, Height: height[h], Weight: b.Weight,
			ChainWork: cw, Timestamp: b.Timestamp, Miner: b.Miner, Status: status,
			Confirmations: conf, TransactionHashes: txHashes,
		}
		snap.Blocks = append(snap.Blocks, bv)
		snap.blockIndex[h] = &snap.Blocks[len(snap.Blocks)-1]
	}

	buildTransactions(s, snap, canonical, orphan, height, tipHeight)

	switch {
	case s.current == 0 && prevTip == "":
		snap.Reason = "initial"
	case canonical[prevTip] && height[prevTip] >= 0:
		snap.Reason = "extended"
	default:
		snap.Reason = "reorg"
	}
	if bestTip == prevTip {
		snap.Reason = "extended"
	}
	if s.current == 0 && len(s.blocks) > 0 {
		snap.Reason = "initial"
	}

	s.current++
	snap.Version = s.current
	s.versions[s.current] = snap
	s.pruneVersionsLocked()
}

func (s *Store) hasBlock(h string) bool { _, ok := s.blocks[h]; return ok }

func buildTransactions(s *Store, snap *Snapshot, canonical, orphan map[string]bool, height map[string]int64, tipHeight int64) {
	type occ struct {
		txHash string
		block  string
		idx    int
	}
	byTx := map[string][]occ{}
	txData := map[string]Transaction{}
	for _, h := range s.order {
		b := s.blocks[h]
		for i, tx := range b.Txs {
			txData[tx.Hash] = tx
			byTx[tx.Hash] = append(byTx[tx.Hash], occ{tx.Hash, h, i})
		}
	}
	txHashes := make([]string, 0, len(byTx))
	for h := range byTx {
		txHashes = append(txHashes, h)
	}
	sort.Strings(txHashes)

	for _, txHash := range txHashes {
		list := byTx[txHash]
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i], list[j]
			if height[a.block] != height[b.block] {
				return height[a.block] < height[b.block]
			}
			return a.block < b.block
		})
		status := TxOrphan
		primary := -1
		for i, o := range list {
			h := o.block
			occStatus := StatusOrphan
			switch {
			case canonical[h]:
				occStatus = StatusCanonical
			case !orphan[h]:
				occStatus = StatusForked
			}
			if canonical[h] && (primary == -1 || height[h] < height[list[primary].block]) {
				primary = i
			}
			if occStatus == StatusForked && status == TxOrphan {
				status = TxForked
			}
		}
		if primary >= 0 {
			status = TxConfirmed
		}
		data := txData[txHash]
		tv := TransactionView{
			Hash: txHash, From: data.From, To: data.To, Value: data.Value,
			Status: status, Duplicate: len(list) > 1,
		}
		for _, o := range list {
			st := StatusOrphan
			switch {
			case canonical[o.block]:
				st = StatusCanonical
			case !orphan[o.block]:
				st = StatusForked
			}
			tv.Occurrences = append(tv.Occurrences, OccurrenceView{
				BlockHash: o.block, Height: height[o.block], Index: o.idx, Status: st,
			})
		}
		if primary >= 0 {
			h := list[primary].block
			tv.BlockHash = h
			tv.BlockHeight = height[h]
			tv.Confirmations = tipHeight - height[h] + 1
		} else {
			// Best forked position for display, but confirmations stay zero.
			h := list[0].block
			tv.BlockHash = h
			tv.BlockHeight = height[h]
		}
		snap.Transactions = append(snap.Transactions, tv)
		snap.txIndex[txHash] = &snap.Transactions[len(snap.Transactions)-1]
	}
}

func betterTip(a, b string, work map[string]uint64, height map[string]int64) bool {
	if work[a] != work[b] {
		return work[a] > work[b]
	}
	if height[a] != height[b] {
		return height[a] > height[b]
	}
	return a < b
}

func saturatingAdd(a, b uint64) uint64 {
	if a+b < a {
		return ^uint64(0)
	}
	return a + b
}

func (s *Store) pruneVersionsLocked() {
	if len(s.versions) <= keepVersions {
		return
	}
	vs := make([]int64, 0, len(s.versions))
	for v := range s.versions {
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i] < vs[j] })
	for _, v := range vs[:len(vs)-keepVersions] {
		delete(s.versions, v)
	}
}
