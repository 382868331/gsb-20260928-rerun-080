package chain

import (
	"fmt"
	"sort"
	"time"

	"chainresearch/internal/model"
)

// BlockIndex 是区块集合的链接索引：高度沿父链计算，
// 区块归属只按实际区块 Hash 判断，不用高度或交易名代替。
type BlockIndex struct {
	byHash   map[string]model.Block
	heightOf map[string]int64
}

func NewBlockIndex(blocks []model.Block) *BlockIndex {
	idx := &BlockIndex{
		byHash:   make(map[string]model.Block, len(blocks)),
		heightOf: make(map[string]int64, len(blocks)),
	}
	for _, b := range blocks {
		idx.byHash[b.Hash] = b
	}
	var resolve func(h string) int64
	resolve = func(h string) int64 {
		if v, ok := idx.heightOf[h]; ok {
			return v
		}
		b := idx.byHash[h]
		if _, ok := idx.byHash[b.PrevHash]; !ok {
			idx.heightOf[h] = 0
		} else {
			idx.heightOf[h] = resolve(b.PrevHash) + 1
		}
		return idx.heightOf[h]
	}
	for h := range idx.byHash {
		resolve(h)
	}
	return idx
}

// Descends 判断 from 沿父链回溯是否经过 target（含自身）。
func (idx *BlockIndex) Descends(from, target string) bool {
	for cur := from; ; {
		if cur == target {
			return true
		}
		b, ok := idx.byHash[cur]
		if !ok {
			return false
		}
		if _, parentExists := idx.byHash[b.PrevHash]; !parentExists {
			return false
		}
		cur = b.PrevHash
	}
}

// AncestryComplete 报告 from 的父链在索引中是否完整可溯源
// （一路回溯到 PrevHash 为空的根）。父块缺失（待补充资料）时
// 返回 false：此时无法证明该块与任何确认边界矛盾。
func (idx *BlockIndex) AncestryComplete(from string) bool {
	for cur := from; ; {
		b, ok := idx.byHash[cur]
		if !ok {
			return false
		}
		if b.PrevHash == "" {
			return true
		}
		if _, parentExists := idx.byHash[b.PrevHash]; !parentExists {
			return false
		}
		cur = b.PrevHash
	}
}

// EffectiveCheckpoint 返回研究线已确认检查点中高度最高者
// （检查点区块必须属于该线，高度由链接关系计算）。
func EffectiveCheckpoint(line *model.ResearchLine, idx *BlockIndex) *model.Checkpoint {
	inLine := map[string]bool{}
	for _, h := range line.Blocks {
		inLine[h] = true
	}
	var best *model.Checkpoint
	var bestHeight int64
	for i := range line.Checkpoints {
		c := line.Checkpoints[i]
		if !inLine[c.Hash] {
			continue
		}
		if _, ok := idx.byHash[c.Hash]; !ok {
			continue
		}
		h := idx.heightOf[c.Hash]
		if best == nil || h > bestHeight || (h == bestHeight && c.Hash < best.Hash) {
			cCopy := c
			best = &cCopy
			bestHeight = h
		}
	}
	return best
}

func sortedHashes(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// CompareLines 比较两条研究线，输出：
//   - 共有事实（两线都包含的区块，按 Hash 身份）；
//   - 各自独有且与对方确认边界兼容的后续资料（可被选择接纳）；
//   - 跨越确认边界的冲突（一方独有区块与另一方已确认检查点矛盾）；
//   - 同一交易在两条线不同分叉里的出现位置（分别说明）。
//
// 某条线更长不会自动撤销另一条线的确认决定：冲突只被记录保留。
func CompareLines(a, b *model.ResearchLine, blocks []model.Block, id string) model.Comparison {
	idx := NewBlockIndex(blocks)
	cmp := model.Comparison{
		ID:          id,
		LineA:       a.ID,
		LineB:       b.ID,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Shared:      []string{},
		OnlyA:       []string{},
		OnlyB:       []string{},
		CompatibleA: []string{},
		CompatibleB: []string{},
		Conflicts:   []model.LineConflict{},
		TxNotes:     []model.TxForkNote{},
	}
	setA, setB := map[string]bool{}, map[string]bool{}
	for _, h := range a.Blocks {
		setA[h] = true
	}
	for _, h := range b.Blocks {
		setB[h] = true
	}
	for h := range setA {
		if setB[h] {
			cmp.Shared = append(cmp.Shared, h)
		} else {
			cmp.OnlyA = append(cmp.OnlyA, h)
		}
	}
	for h := range setB {
		if !setA[h] {
			cmp.OnlyB = append(cmp.OnlyB, h)
		}
	}
	sort.Strings(cmp.Shared)
	sort.Strings(cmp.OnlyA)
	sort.Strings(cmp.OnlyB)

	cpA := EffectiveCheckpoint(a, idx)
	cpB := EffectiveCheckpoint(b, idx)
	// 跨越确认边界：区块与对方的生效检查点互不互为祖先（真正分叉），
	// 而不是检查点之前的历史或之后的正常延伸。父块缺失的待补充资料
	// 无法证明矛盾：先作为可接纳的待补充项保留（带出处），父块迟到后
	// 再重新判断；只有祖先链完整且确实不包含检查点时才判定为冲突。
	crosses := func(h string, cp *model.Checkpoint) bool {
		if cp == nil || h == cp.Hash {
			return false
		}
		if idx.Descends(h, cp.Hash) || idx.Descends(cp.Hash, h) {
			return false
		}
		return idx.AncestryComplete(h) && idx.AncestryComplete(cp.Hash)
	}
	conflictSet := map[string]bool{}
	for _, h := range cmp.OnlyA {
		if crosses(h, cpB) {
			conflictSet[h] = true
			cmp.Conflicts = append(cmp.Conflicts, model.LineConflict{
				Hash: h, InLine: a.ID, CrossesLine: b.ID, Checkpoint: cpB.Hash,
			})
		} else {
			cmp.CompatibleA = append(cmp.CompatibleA, h)
		}
	}
	for _, h := range cmp.OnlyB {
		if crosses(h, cpA) {
			conflictSet[h] = true
			cmp.Conflicts = append(cmp.Conflicts, model.LineConflict{
				Hash: h, InLine: b.ID, CrossesLine: a.ID, Checkpoint: cpA.Hash,
			})
		} else {
			cmp.CompatibleB = append(cmp.CompatibleB, h)
		}
	}
	sort.Slice(cmp.Conflicts, func(i, j int) bool { return cmp.Conflicts[i].Hash < cmp.Conflicts[j].Hash })

	// 同一交易在两条线中的出现位置分别列出（按所在区块 Hash）。
	txInA := map[string]map[string]bool{}
	txInB := map[string]map[string]bool{}
	for _, blk := range blocks {
		for _, tx := range blk.Txs {
			if setA[blk.Hash] {
				if txInA[tx.TxID] == nil {
					txInA[tx.TxID] = map[string]bool{}
				}
				txInA[tx.TxID][blk.Hash] = true
			}
			if setB[blk.Hash] {
				if txInB[tx.TxID] == nil {
					txInB[tx.TxID] = map[string]bool{}
				}
				txInB[tx.TxID][blk.Hash] = true
			}
		}
	}
	txIDs := map[string]bool{}
	for id := range txInA {
		txIDs[id] = true
	}
	for id := range txInB {
		txIDs[id] = true
	}
	for _, id := range sortedHashes(txIDs) {
		inA, inB := txInA[id], txInB[id]
		if len(inA) == 0 || len(inB) == 0 {
			continue
		}
		note := model.TxForkNote{
			TxID:      id,
			BlocksInA: sortedHashes(inA),
			BlocksInB: sortedHashes(inB),
		}
		cmp.TxNotes = append(cmp.TxNotes, note)
	}
	return cmp
}

// MergeSpec 描述一次合并：保留哪一条线的确认、接纳哪些兼容的
// 后续资料（必须是比较结果中兼容集合的子集）。
type MergeSpec struct {
	Name             string
	KeepConfirmation string // "a" 或 "b"：保留哪一条研究线的确认
	Accept           []string
}

// MergeLines 依据一次已持久化的比较把两条研究线合并为新线：
//   - 共有事实与被选中的兼容资料进入新线，每个区块记录采用决定
//     （来自哪次比较、哪条来源线），保存重开后可复核；
//   - 跨越保留方确认边界的冲突部分记入 Deferred（暂不采用），
//     不会被拼接进新链，也不会被标记为无效；
//   - 原有两条线及其确认依据保持不变，可继续回看；
//   - 接受部分后续资料时不补齐缺失的中间资料：缺口由
//     BuildLineSnapshot 以 PendingGap 形式保留待补充。
func MergeLines(a, b *model.ResearchLine, cmp model.Comparison, spec MergeSpec, newID string) (*model.ResearchLine, error) {
	return nil, fmt.Errorf("合并确认范围需要区块内容，请使用 MergeLinesWithBlocks")
}

// MergeLinesWithBlocks 与 MergeLines 相同，但使用实际区块内容计算父链，
// 以完整继承选定研究线的确认范围。
func MergeLinesWithBlocks(a, b *model.ResearchLine, blocks []model.Block, cmp model.Comparison, spec MergeSpec, newID string) (*model.ResearchLine, error) {
	if cmp.LineA != a.ID || cmp.LineB != b.ID {
		return nil, fmt.Errorf("比较 %s 不是研究线 %s 与 %s 的结果", cmp.ID, a.ID, b.ID)
	}
	if spec.KeepConfirmation != "a" && spec.KeepConfirmation != "b" {
		return nil, fmt.Errorf("需要选择保留哪一条研究线的确认（a 或 b）")
	}
	idx := NewBlockIndex(blocks)
	compatible := map[string]string{} // hash -> 来源线 ID
	for _, h := range cmp.CompatibleA {
		compatible[h] = a.ID
	}
	for _, h := range cmp.CompatibleB {
		compatible[h] = b.ID
	}
	keep, other := a, b
	if spec.KeepConfirmation == "b" {
		keep, other = b, a
	}
	keepBlocks := map[string]bool{}
	for _, h := range keep.Blocks {
		keepBlocks[h] = true
	}
	shared := map[string]bool{}
	for _, h := range cmp.Shared {
		shared[h] = true
	}
	keepCP := EffectiveCheckpoint(keep, idx)
	// 选中某条线的确认即继承该确认覆盖的完整范围，即使这些独有确认块
	// 未出现在 accept 中，或在双向比较中被另一条线的检查点标为冲突。
	if keepCP != nil {
		for _, h := range cmp.CompatibleA {
			if keepBlocks[h] && idx.Descends(h, keepCP.Hash) {
				compatible[h] = keep.ID
			}
		}
		for _, h := range cmp.CompatibleB {
			if keepBlocks[h] && idx.Descends(h, keepCP.Hash) {
				compatible[h] = keep.ID
			}
		}
		for _, c := range cmp.Conflicts {
			if c.InLine == keep.ID && idx.Descends(c.Hash, keepCP.Hash) {
				compatible[c.Hash] = keep.ID
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	merged := &model.ResearchLine{
		ID:             newID,
		Name:           spec.Name,
		CreatedAt:      now,
		ParentLines:    []string{a.ID, b.ID},
		BaseComparison: cmp.ID,
		Blocks:         []string{},
	}
	adopted := map[string]bool{}
	addBlock := func(h, sourceLine string) {
		if adopted[h] {
			return
		}
		adopted[h] = true
		merged.Blocks = append(merged.Blocks, h)
		merged.Adoptions = append(merged.Adoptions, model.Adoption{
			Hash: h, ComparisonID: cmp.ID, SourceLine: sourceLine, AdoptedAt: now,
		})
	}
	for _, h := range cmp.Shared {
		addBlock(h, "both")
	}
	if keepCP != nil {
		for _, h := range keep.Blocks {
			if !idx.Descends(keepCP.Hash, h) {
				continue
			}
			source := keep.ID
			if shared[h] {
				source = "both"
			}
			addBlock(h, source)
		}
	}
	seenAccept := map[string]bool{}
	for _, h := range spec.Accept {
		if seenAccept[h] {
			continue
		}
		seenAccept[h] = true
		source, ok := compatible[h]
		if !ok {
			return nil, fmt.Errorf("区块 %s 不在两线兼容后续资料中，不能接纳（冲突部分只能暂缓）", h)
		}
		addBlock(h, source)
	}
	sort.Strings(merged.Blocks)

	// 另一方的确认不被撤销，仍留在原研究线上可回看；新线复制选中线
	// 已进入确认范围的检查点记录及其确认依据。
	if keepCP != nil {
		for _, c := range keep.Checkpoints {
			if adopted[c.Hash] {
				merged.Checkpoints = append(merged.Checkpoints, c)
			}
		}
	}

	// 仅暂缓“对方资料越过新线所选检查点”的冲突。选中线自己的冲突标记
	// 是相对未选检查点产生的，不能顺带删除或错误暂缓甲方确认。
	deferredSeen := map[string]bool{}
	for _, c := range cmp.Conflicts {
		if c.InLine != other.ID {
			continue
		}
		if keepCP != nil && c.Checkpoint != keepCP.Hash {
			continue
		}
		if deferredSeen[c.Hash] {
			continue
		}
		deferredSeen[c.Hash] = true
		checkpointHash := ""
		if keepCP != nil {
			checkpointHash = keepCP.Hash
		}
		merged.Deferred = append(merged.Deferred, model.Deferred{
			Hash:         c.Hash,
			ComparisonID: cmp.ID,
			Reason: fmt.Sprintf(
				"跨越研究线 %s 已确认检查点 %s 的冲突资料，暂不采用（仍有效，待研究者裁决）",
				keep.ID, checkpointHash),
		})
	}
	if keepCP == nil {
		for _, c := range cmp.Conflicts {
			if deferredSeen[c.Hash] {
				continue
			}
			deferredSeen[c.Hash] = true
			merged.Deferred = append(merged.Deferred, model.Deferred{
				Hash:         c.Hash,
				ComparisonID: cmp.ID,
				Reason: fmt.Sprintf(
					"跨越研究线 %s 已确认检查点 %s 的冲突资料，暂不采用（仍有效，待研究者裁决）",
					c.CrossesLine, c.Checkpoint),
			})
		}
	}
	return merged, nil
}

// BuildLineSnapshot 为研究线重建一个不可变版本快照：
// 只使用该线包含的区块与该线自己的检查点，并附带研究线视图
// （采用审计、待补充范围、暂缓冲突、撤回记录）。
// 待补充范围按当前区块集合现算：迟到的共同祖先材料到达后
// 缺口自动消失；缺少的中间资料不会被拼接成从未存在的链。
func BuildLineSnapshot(line *model.ResearchLine, allBlocks []model.Block, nextVersion int64, label string, resolutions map[string]string) model.VersionSnapshot {
	inLine := map[string]bool{}
	for _, h := range line.Blocks {
		inLine[h] = true
	}
	var lineBlocks []model.Block
	for _, b := range allBlocks {
		if inLine[b.Hash] {
			lineBlocks = append(lineBlocks, b)
		}
	}
	snap := BuildSnapshot(lineBlocks, nextVersion, label, line.Checkpoints, resolutions)
	snap.LineID = line.ID

	view := &model.LineView{
		ID:             line.ID,
		Name:           line.Name,
		ParentLines:    line.ParentLines,
		BaseComparison: line.BaseComparison,
		Adoptions:      map[string]model.Adoption{},
		PendingGaps:    []model.PendingGap{},
		Deferred:       line.Deferred,
		Withdrawals:    line.Withdrawals,
	}
	for _, a := range line.Adoptions {
		if inLine[a.Hash] {
			view.Adoptions[a.Hash] = a
		}
	}
	if view.Deferred == nil {
		view.Deferred = []model.Deferred{}
	}
	if view.Withdrawals == nil {
		view.Withdrawals = []model.Withdrawal{}
	}

	// 待补充范围：已采用区块缺少的父区块与引用目标所需的中间资料。
	txPresent := map[string]bool{}
	for _, b := range lineBlocks {
		for _, tx := range b.Txs {
			txPresent[tx.TxID] = true
		}
	}
	for _, b := range lineBlocks {
		gap := model.PendingGap{Hash: b.Hash}
		if adoption, ok := lineAdoption(line, b.Hash); ok {
			gap.SourceLine = adoption.SourceLine
		}
		if b.PrevHash != "" && !inLine[b.PrevHash] {
			gap.MissingParents = []string{b.PrevHash}
		}
		missingRefs := map[string]bool{}
		for _, tx := range b.Txs {
			for _, in := range tx.Inputs {
				if !txPresent[in.TxID] {
					missingRefs[in.TxID] = true
				}
			}
		}
		gap.MissingRefs = sortedHashes(missingRefs)
		if len(gap.MissingParents) > 0 || len(gap.MissingRefs) > 0 {
			view.PendingGaps = append(view.PendingGaps, gap)
		}
	}
	sort.Slice(view.PendingGaps, func(i, j int) bool { return view.PendingGaps[i].Hash < view.PendingGaps[j].Hash })

	snap.Line = view
	return snap
}

func lineAdoption(line *model.ResearchLine, hash string) (model.Adoption, bool) {
	for _, adoption := range line.Adoptions {
		if adoption.Hash == hash {
			return adoption, true
		}
	}
	return model.Adoption{}, false
}
