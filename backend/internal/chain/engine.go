package chain

import (
	"sort"
	"time"

	"chainresearch/internal/model"
)

// BuildSnapshot 依据当前全部区块重建不可变索引版本。
// 主链选择规则：累计工作量最大的链为主链；工作量相同时
// 以端点 Hash 字典序较小者为准，保证结果确定可复现。
// 新主链产生后，旧分叉区块 canonical=false 且 confirmations=0，
// 但仍保留在快照中可追溯；交易位置与确认状态随之整体重算。
func BuildSnapshot(blocks []model.Block, nextVersion int64, label string) model.VersionSnapshot {
	return buildSnapshot(blocks, nextVersion, label, nil, nil, "")
}

// BuildSnapshotWithCheckpoint 在研究者确认的检查点约束下重建索引版本：
//   - 当前较优分叉仍包含检查点：一切如常，检查点及以前为确认范围，
//     之后为暂定范围，分叉变化可继续更新；
//   - 新资料提出越过检查点的更重竞争链：不自动撤销原确认，主链钉在
//     包含检查点的链上，竞争链及其引用差异作为冲突保留在快照中，
//     除非该竞争链已被研究者登记为独立研究线。
func BuildSnapshotWithCheckpoint(blocks []model.Block, nextVersion int64, label string, cp *model.Checkpoint, researchLines []model.ResearchLine) model.VersionSnapshot {
	return buildSnapshot(blocks, nextVersion, label, cp, researchLines, "")
}

func buildSnapshot(blocks []model.Block, nextVersion int64, label string, cp *model.Checkpoint, researchLines []model.ResearchLine, forcedTip string) model.VersionSnapshot {
	snap := model.VersionSnapshot{
		Version:   nextVersion,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Label:     label,
		Blocks:    []model.BlockView{},
		TxIndex:   []model.TxPosition{},
		TxRefs:    []model.TxRef{},
	}
	if len(blocks) == 0 {
		return snap
	}

	byHash := make(map[string]model.Block, len(blocks))
	heightOf := make(map[string]int64, len(blocks))
	children := map[string][]string{}
	tips := map[string]bool{}
	for _, b := range blocks {
		byHash[b.Hash] = b
		tips[b.Hash] = true
	}
	for _, b := range blocks {
		if _, ok := byHash[b.PrevHash]; ok {
			children[b.PrevHash] = append(children[b.PrevHash], b.Hash)
			delete(tips, b.PrevHash)
		}
	}
	// 高度沿父链计算；缺少父块的块视为链根（独立来源的链段）。
	var resolve func(h string) int64
	resolve = func(h string) int64 {
		if v, ok := heightOf[h]; ok {
			return v
		}
		b := byHash[h]
		if _, ok := byHash[b.PrevHash]; !ok {
			heightOf[h] = 0
		} else {
			heightOf[h] = resolve(b.PrevHash) + 1
		}
		return heightOf[h]
	}
	for h := range byHash {
		resolve(h)
	}

	// 从每个端点回溯，选出主链端点。
	type chainInfo struct {
		tip  string
		work int64
	}
	tipList := make([]string, 0, len(tips))
	for t := range tips {
		tipList = append(tipList, t)
	}
	sort.Strings(tipList)
	var candidates []chainInfo
	for _, tip := range tipList {
		var totalWork int64
		cur := tip
		for {
			b, ok := byHash[cur]
			if !ok {
				break
			}
			totalWork += b.Work
			if _, parentExists := byHash[b.PrevHash]; !parentExists {
				break
			}
			cur = b.PrevHash
		}
		candidates = append(candidates, chainInfo{tip: tip, work: totalWork})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].work != candidates[j].work {
			return candidates[i].work > candidates[j].work
		}
		return candidates[i].tip < candidates[j].tip
	})

	// 检查点约束下的主链端点选择。
	bestTip := candidates[0].tip
	tipHash := bestTip
	if forcedTip != "" {
		tipHash = forcedTip
	}
	researchTips := map[string]bool{}
	for _, rl := range researchLines {
		researchTips[rl.TipHash] = true
	}
	var cpView *model.CheckpointView
	var conflict *model.CheckpointConflict
	cpHeight := int64(-1)
	if cp != nil {
		if _, ok := byHash[cp.BlockHash]; ok {
			cpHeight = heightOf[cp.BlockHash]
		}
	}
	if cp != nil && cpHeight >= 0 && forcedTip == "" {
		cpView = &model.CheckpointView{
			BlockHash:   cp.BlockHash,
			Height:      cp.Height,
			ConfirmedAt: cp.ConfirmedAt,
			Note:        cp.Note,
			Basis:       cp.Basis,
			Status:      "ok",
		}
		if !chainContains(byHash, bestTip, cp.BlockHash) {
			// 较优竞争链越过了检查点：不自动撤销确认，钉住主链。
			pinned := ""
			for _, c := range candidates {
				if chainContains(byHash, c.tip, cp.BlockHash) {
					pinned = c.tip
					break
				}
			}
			if pinned == "" {
				pinned = cp.BlockHash
			}
			tipHash = pinned
			if !researchTips[bestTip] {
				conflict = &model.CheckpointConflict{
					RivalTipHash:   bestTip,
					RivalTipHeight: heightOf[bestTip],
					RivalWork:      candidates[0].work,
					DivergeHeight:  -1,
				}
			}
		}
	}
	snap.TipHash = tipHash
	snap.TipHeight = heightOf[tipHash]

	canonical := map[string]bool{}
	for cur := tipHash; ; {
		canonical[cur] = true
		b := byHash[cur]
		if _, ok := byHash[b.PrevHash]; !ok {
			break
		}
		cur = b.PrevHash
	}

	// 冲突分叉点：竞争链与钉住主链的最近公共块高度。
	if conflict != nil {
		for cur := bestTip; ; {
			if canonical[cur] {
				conflict.DivergeHeight = heightOf[cur]
				break
			}
			b, ok := byHash[cur]
			if !ok {
				break
			}
			if _, parentOk := byHash[b.PrevHash]; !parentOk {
				break
			}
			cur = b.PrevHash
		}
	}

	// 链范围：检查点及以前的主链为确认范围；越过检查点的分叉
	// （与主链的最近公共块低于检查点）为冲突范围；其余为暂定范围。
	rangeCache := map[string]string{}
	var rangeOf func(h string) string
	rangeOf = func(h string) string {
		if cpView == nil {
			return ""
		}
		if r, ok := rangeCache[h]; ok {
			return r
		}
		var r string
		switch {
		case canonical[h] && heightOf[h] <= cpHeight:
			r = model.RangeConfirmed
		case canonical[h]:
			r = model.RangeTentative
		default:
			r = model.RangeTentative
			for cur := h; ; {
				b := byHash[cur]
				if _, ok := byHash[b.PrevHash]; !ok {
					break
				}
				if canonical[b.PrevHash] {
					if heightOf[b.PrevHash] < cpHeight {
						r = model.RangeConflict
					}
					break
				}
				cur = b.PrevHash
			}
		}
		rangeCache[h] = r
		return r
	}

	// 分叉编号：从非主链块回溯，找到的第一个非主链块（紧邻分歧点）
	// 的 Hash 即该分叉 ID，同一条分叉上的块共享此 ID。
	branchCache := map[string]string{}
	branchOf := func(h string) string {
		if canonical[h] {
			return "main"
		}
		if id, ok := branchCache[h]; ok {
			return id
		}
		root := h
		for {
			b := byHash[root]
			if b.PrevHash == "" {
				break
			}
			if _, ok := byHash[b.PrevHash]; !ok {
				break
			}
			if canonical[b.PrevHash] {
				break
			}
			root = b.PrevHash
		}
		id := "fork-" + root
		branchCache[h] = id
		return id
	}

	blockHashList := make([]string, 0, len(byHash))
	for h := range byHash {
		blockHashList = append(blockHashList, h)
	}
	sort.Slice(blockHashList, func(i, j int) bool {
		if heightOf[blockHashList[i]] != heightOf[blockHashList[j]] {
			return heightOf[blockHashList[i]] < heightOf[blockHashList[j]]
		}
		return blockHashList[i] < blockHashList[j]
	})

	for _, h := range blockHashList {
		b := byHash[h]
		isCanon := canonical[h]
		var conf int64
		if isCanon {
			conf = snap.TipHeight - heightOf[h] + 1
		}
		view := model.BlockView{
			Hash:          b.Hash,
			PrevHash:      b.PrevHash,
			Height:        heightOf[h],
			Canonical:     isCanon,
			BranchID:      branchOf(h),
			Range:         rangeOf(h),
			Confirmations: conf,
			Work:          b.Work,
			Miner:         b.Miner,
			Timestamp:     b.Timestamp,
			Txs:           []model.TxInBlock{},
		}
		for i, tx := range b.Txs {
			view.Txs = append(view.Txs, model.TxInBlock{TxID: tx.TxID, Index: i, Memo: tx.Memo})
			snap.TxIndex = append(snap.TxIndex, model.TxPosition{
				TxID:          tx.TxID,
				BlockHash:     h,
				Index:         i,
				Height:        heightOf[h],
				Canonical:     isCanon,
				Range:         view.Range,
				Confirmations: conf,
				BranchID:      view.BranchID,
			})
		}
		snap.Blocks = append(snap.Blocks, view)
	}

	sort.SliceStable(snap.TxIndex, func(i, j int) bool {
		if snap.TxIndex[i].TxID != snap.TxIndex[j].TxID {
			return snap.TxIndex[i].TxID < snap.TxIndex[j].TxID
		}
		if snap.TxIndex[i].Height != snap.TxIndex[j].Height {
			return snap.TxIndex[i].Height < snap.TxIndex[j].Height
		}
		return snap.TxIndex[i].Index < snap.TxIndex[j].Index
	})

	// 交易引用关系：input 指向被花费的前序交易。
	// 同一交易在每个分叉位置各产生一条引用，标注所在区块、链视角
	// 与当前有效性；重复导入同一批区块不会产生重复引用（区块已按
	// Hash 去重，且此处按 引用方位置+目标 去重）。
	posByTx := map[string][]model.TxPosition{}
	for _, p := range snap.TxIndex {
		posByTx[p.TxID] = append(posByTx[p.TxID], p)
	}
	// 解析目标位置：优先主链位置（当前链视角），否则取高度最低的
	// 分叉位置并标记 ToCanonical=false，此时引用不算当前有效。
	resolveTarget := func(txID string) (model.TxPosition, bool) {
		positions := posByTx[txID]
		if len(positions) == 0 {
			return model.TxPosition{}, false
		}
		best := positions[0]
		for _, p := range positions {
			if p.Canonical {
				return p, true
			}
			if p.Height < best.Height {
				best = p
			}
		}
		return best, true
	}
	refSeen := map[string]bool{}
	for _, h := range blockHashList {
		b := byHash[h]
		for _, tx := range b.Txs {
			for _, in := range tx.Inputs {
				key := tx.TxID + "@" + h + "->" + in.TxID
				if refSeen[key] {
					continue
				}
				refSeen[key] = true
				ref := model.TxRef{
					FromTxID:      tx.TxID,
					ToTxID:        in.TxID,
					Vout:          in.Vout,
					Scope:         rangeOf(h),
					FromBlockHash: h,
					FromBranchID:  branchOf(h),
					FromCanonical: canonical[h],
				}
				if canonical[h] {
					ref.FromConfirmations = snap.TipHeight - heightOf[h] + 1
				}
				if target, ok := resolveTarget(in.TxID); ok {
					ref.Resolved = true
					ref.ToBlockHash = target.BlockHash
					ref.ToBranchID = target.BranchID
					ref.ToCanonical = target.Canonical
					ref.ToConfirmations = target.Confirmations
					// 目标落在冲突范围时，整条引用按冲突范围对待。
					if rangeOf(target.BlockHash) == model.RangeConflict {
						ref.Scope = model.RangeConflict
					}
				}
				// 仅当引用方与目标都在当前主链上时才算当前有效；
				// 目标尚未到达或只存在于另一分叉时不提前认作有效。
				ref.Active = ref.Resolved && ref.FromCanonical && ref.ToCanonical
				snap.TxRefs = append(snap.TxRefs, ref)
			}
		}
	}
	sort.SliceStable(snap.TxRefs, func(i, j int) bool {
		if snap.TxRefs[i].FromTxID != snap.TxRefs[j].FromTxID {
			return snap.TxRefs[i].FromTxID < snap.TxRefs[j].FromTxID
		}
		if snap.TxRefs[i].ToTxID != snap.TxRefs[j].ToTxID {
			return snap.TxRefs[i].ToTxID < snap.TxRefs[j].ToTxID
		}
		return snap.TxRefs[i].FromBlockHash < snap.TxRefs[j].FromBlockHash
	})

	// 冲突保留：钉住视角与竞争链视角下的引用差异一并存入快照，
	// 供研究者裁决时核对，系统不自动撤销原确认。
	if cpView != nil {
		if conflict != nil {
			rivalSnap := buildSnapshot(blocks, nextVersion, label, nil, nil, bestTip)
			diff := DiffRefs(snap, rivalSnap)
			conflict.RefChanges = &diff
			cpView.Conflict = conflict
			cpView.Status = "conflict"
		}
		snap.Checkpoint = cpView
	}
	if len(researchLines) > 0 {
		snap.ResearchLines = append([]model.ResearchLine{}, researchLines...)
	}

	return snap
}

// chainContains 判断从 tip 回溯的链上是否包含 target。
func chainContains(byHash map[string]model.Block, tip, target string) bool {
	for cur := tip; ; {
		if cur == target {
			return true
		}
		b, ok := byHash[cur]
		if !ok {
			return false
		}
		if _, parentOk := byHash[b.PrevHash]; !parentOk {
			return false
		}
		cur = b.PrevHash
	}
}
