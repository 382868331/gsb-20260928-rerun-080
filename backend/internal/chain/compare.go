package chain

import (
	"sort"

	"chainresearch/internal/model"
)

// CompareStudies 对比两条研究线的最新快照：
//   - 共有事实：按实际区块身份（Hash）判断两线共有的区块，
//     不用相同高度或交易名称代替；
//   - 各自独有的后续资料：相对另一线分为兼容后续（延续对方已有
//     历史且不跨越其确认边界）、跨越确认边界的冲突、缺少中间资料
//     的断段；
//   - 各自独有的交易引用：同一交易在不同分叉里的引用分别列出。
//
// blocksA/blocksB 是两线各自纳入的原始区块（用于沿父链判断祖先关系）。
func CompareStudies(snapA, snapB model.VersionSnapshot, blocksA, blocksB []model.Block) model.CompareResult {
	result := model.CompareResult{
		StudyA:      snapA.Study,
		StudyB:      snapB.Study,
		CheckpointA: snapA.Checkpoint,
		CheckpointB: snapB.Checkpoint,
		Shared:      []model.ComparedBlock{},
		OnlyA:       []model.ComparedBlock{},
		OnlyB:       []model.ComparedBlock{},
		RefsOnlyA:   []model.TxRef{},
		RefsOnlyB:   []model.TxRef{},
	}

	inA := map[string]bool{}
	inB := map[string]bool{}
	viewA := map[string]model.BlockView{}
	viewB := map[string]model.BlockView{}
	for _, b := range snapA.Blocks {
		inA[b.Hash] = true
		viewA[b.Hash] = b
	}
	for _, b := range snapB.Blocks {
		inB[b.Hash] = true
		viewB[b.Hash] = b
	}

	// 祖先判断基于两线区块的并集（按 Hash 身份），迟到补充的共同
	// 祖先材料导入任一线后，下一次比较即可识别出衔接关系。
	byHash := map[string]model.Block{}
	for _, b := range blocksA {
		byHash[b.Hash] = b
	}
	for _, b := range blocksB {
		byHash[b.Hash] = b
	}
	descendsFrom := func(from, target string) bool {
		for cur := from; ; {
			if cur == target {
				return true
			}
			b, ok := byHash[cur]
			if !ok {
				return false
			}
			if _, parentExists := byHash[b.PrevHash]; !parentExists {
				return false
			}
			cur = b.PrevHash
		}
	}
	// relationOf 判断 h（只属于其中一线）相对另一线的关系。
	relationOf := func(h string, otherIn map[string]bool, otherCp *model.CheckpointView) string {
		if otherCp != nil && otherIn[otherCp.Hash] && !descendsFrom(h, otherCp.Hash) {
			// 不经过对方已确认的检查点：跨越确认边界的冲突，
			// 不能因为该线更长就自动撤销对方的确认决定。
			return model.RelConflict
		}
		// 沿父链回溯，若能接到对方已有的区块则为兼容后续。
		for cur := h; ; {
			b, ok := byHash[cur]
			if !ok {
				break
			}
			parent := b.PrevHash
			if parent == "" {
				break
			}
			if otherIn[parent] {
				return model.RelCompatible
			}
			if _, parentKnown := byHash[parent]; !parentKnown {
				break
			}
			cur = parent
		}
		// 祖先链在两线资料中都接不上：缺少中间资料。
		return model.RelDisconnected
	}

	for _, b := range snapA.Blocks {
		if inB[b.Hash] {
			result.Shared = append(result.Shared, model.ComparedBlock{
				Hash: b.Hash, PrevHash: b.PrevHash, Height: b.Height,
				Scope: b.Scope, Relation: model.RelShared, Canonical: b.Canonical,
			})
			continue
		}
		result.OnlyA = append(result.OnlyA, model.ComparedBlock{
			Hash: b.Hash, PrevHash: b.PrevHash, Height: b.Height,
			Scope: b.Scope, Relation: relationOf(b.Hash, inB, snapB.Checkpoint), Canonical: b.Canonical,
		})
	}
	for _, b := range snapB.Blocks {
		if inA[b.Hash] {
			continue
		}
		result.OnlyB = append(result.OnlyB, model.ComparedBlock{
			Hash: b.Hash, PrevHash: b.PrevHash, Height: b.Height,
			Scope: b.Scope, Relation: relationOf(b.Hash, inA, snapA.Checkpoint), Canonical: b.Canonical,
		})
	}
	sortCompared := func(list []model.ComparedBlock) {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Height != list[j].Height {
				return list[i].Height < list[j].Height
			}
			return list[i].Hash < list[j].Hash
		})
	}
	sortCompared(result.Shared)
	sortCompared(result.OnlyA)
	sortCompared(result.OnlyB)

	// 引用差异：按 引用方交易@所在区块->目标交易 为键，
	// 同一交易在不同分叉里的引用分别列出。
	refKey := func(r model.TxRef) string {
		return r.FromTxID + "@" + r.FromBlockHash + "->" + r.ToTxID
	}
	refsA := map[string]bool{}
	for _, r := range snapA.TxRefs {
		refsA[refKey(r)] = true
	}
	refsB := map[string]bool{}
	for _, r := range snapB.TxRefs {
		refsB[refKey(r)] = true
	}
	for _, r := range snapA.TxRefs {
		if !refsB[refKey(r)] {
			result.RefsOnlyA = append(result.RefsOnlyA, r)
		}
	}
	for _, r := range snapB.TxRefs {
		if !refsA[refKey(r)] {
			result.RefsOnlyB = append(result.RefsOnlyB, r)
		}
	}
	return result
}
