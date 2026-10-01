package chain

import (
	"fmt"
	"sort"

	"chainresearch/internal/model"
)

// DiffRefs 比较两个索引版本的引用关系，把变化分为：
//   - position_migrated：同一引用（引用方+目标+vout）的引用方区块位置迁移；
//   - target_changed：同一引用方 input（引用方+vout）指向的目标交易变化，
//     或目标解析到的位置/链视角变化；
//   - confirmations_only：位置与目标都不变，仅确认数变化；
//   - added / removed：仅出现在其中一个版本的引用。
//
// 结果按引用方、目标排序，保证输出确定。
func DiffRefs(oldSnap, newSnap model.VersionSnapshot) model.RefDiff {
	diff := model.RefDiff{
		FromVersion: oldSnap.Version,
		ToVersion:   newSnap.Version,
		Entries:     []model.RefDiffEntry{},
	}

	exactKey := func(r model.TxRef) string {
		return fmt.Sprintf("%s@%s|%s|%d", r.FromTxID, r.FromBlockHash, r.ToTxID, r.Vout)
	}
	refKey := func(r model.TxRef) string {
		return fmt.Sprintf("%s|%s|%d", r.FromTxID, r.ToTxID, r.Vout)
	}
	inputKey := func(r model.TxRef) string {
		return fmt.Sprintf("%s|%d", r.FromTxID, r.Vout)
	}

	oldExact := map[string]model.TxRef{}
	for _, r := range oldSnap.TxRefs {
		oldExact[exactKey(r)] = r
	}
	newExact := map[string]model.TxRef{}
	for _, r := range newSnap.TxRefs {
		newExact[exactKey(r)] = r
	}

	// 位置迁移预配对：同一引用在两版本中的主链（当前有效）位置不同，
	// 说明引用方交易的有效位置随分叉切换发生了迁移。
	consumedOld := map[string]bool{}
	consumedNew := map[string]bool{}
	oldActive := map[string]model.TxRef{}
	for _, r := range oldSnap.TxRefs {
		if r.FromCanonical {
			oldActive[refKey(r)] = r
		}
	}
	newActive := map[string]model.TxRef{}
	for _, r := range newSnap.TxRefs {
		if r.FromCanonical {
			newActive[refKey(r)] = r
		}
	}
	for key, oldRef := range oldActive {
		newRef, ok := newActive[key]
		if !ok || newRef.FromBlockHash == oldRef.FromBlockHash {
			continue
		}
		oldCopy := oldRef
		newCopy := newRef
		diff.Entries = append(diff.Entries, model.RefDiffEntry{
			Kind:   "position_migrated",
			Before: &oldCopy,
			After:  &newCopy,
		})
		consumedOld[exactKey(oldRef)] = true
		consumedNew[exactKey(newRef)] = true
	}

	var oldRest, newRest []model.TxRef
	for key, oldRef := range oldExact {
		if consumedOld[key] {
			continue
		}
		newRef, ok := newExact[key]
		if !ok || consumedNew[key] {
			oldRest = append(oldRest, oldRef)
			continue
		}
		if entry := classifySamePosition(oldRef, newRef); entry != nil {
			diff.Entries = append(diff.Entries, *entry)
		}
	}
	for key, newRef := range newExact {
		if consumedNew[key] {
			continue
		}
		if _, ok := oldExact[key]; !ok || consumedOld[key] {
			newRest = append(newRest, newRef)
		}
	}

	// 位置迁移：同一引用（引用方+目标+vout）出现在不同区块。
	oldRest, newRest = pairBy(oldRest, newRest, refKey, "position_migrated", &diff)
	// 目标变化：同一引用方 input 指向了不同的目标交易。
	oldRest, newRest = pairBy(oldRest, newRest, inputKey, "target_changed", &diff)

	for _, r := range oldRest {
		oldCopy := r
		diff.Entries = append(diff.Entries, model.RefDiffEntry{Kind: "removed", Before: &oldCopy})
	}
	for _, r := range newRest {
		newCopy := r
		diff.Entries = append(diff.Entries, model.RefDiffEntry{Kind: "added", After: &newCopy})
	}

	sort.SliceStable(diff.Entries, func(i, j int) bool {
		ei, ej := diff.Entries[i], diff.Entries[j]
		if fi, fj := entryFrom(ei), entryFrom(ej); fi != fj {
			return fi < fj
		}
		return entryKindOrder(ei.Kind) < entryKindOrder(ej.Kind)
	})
	return diff
}

// pairBy 按 key 在两侧剩余引用之间配对，配对成功记为 kind 类变化。
func pairBy(oldRest, newRest []model.TxRef, keyOf func(model.TxRef) string, kind string, diff *model.RefDiff) ([]model.TxRef, []model.TxRef) {
	newByKey := map[string][]int{}
	for i, r := range newRest {
		k := keyOf(r)
		newByKey[k] = append(newByKey[k], i)
	}
	usedNew := map[int]bool{}
	var oldLeft []model.TxRef
	for _, oldRef := range oldRest {
		idx := -1
		for _, i := range newByKey[keyOf(oldRef)] {
			if !usedNew[i] {
				idx = i
				break
			}
		}
		if idx < 0 {
			oldLeft = append(oldLeft, oldRef)
			continue
		}
		usedNew[idx] = true
		oldCopy := oldRef
		newCopy := newRest[idx]
		diff.Entries = append(diff.Entries, model.RefDiffEntry{Kind: kind, Before: &oldCopy, After: &newCopy})
	}
	var newLeft []model.TxRef
	for i, r := range newRest {
		if !usedNew[i] {
			newLeft = append(newLeft, r)
		}
	}
	return oldLeft, newLeft
}

// classifySamePosition 对两版本中位置相同的同一引用分类；无变化返回 nil。
func classifySamePosition(oldRef, newRef model.TxRef) *model.RefDiffEntry {
	oldCopy := oldRef
	newCopy := newRef
	switch {
	case oldRef.Resolved != newRef.Resolved ||
		oldRef.ToBlockHash != newRef.ToBlockHash ||
		oldRef.ToCanonical != newRef.ToCanonical ||
		oldRef.FromCanonical != newRef.FromCanonical:
		return &model.RefDiffEntry{Kind: "target_changed", Before: &oldCopy, After: &newCopy}
	case oldRef.FromConfirmations != newRef.FromConfirmations ||
		oldRef.ToConfirmations != newRef.ToConfirmations:
		return &model.RefDiffEntry{Kind: "confirmations_only", Before: &oldCopy, After: &newCopy}
	}
	return nil
}

func entryFrom(e model.RefDiffEntry) string {
	if e.Before != nil {
		return e.Before.FromTxID
	}
	if e.After != nil {
		return e.After.FromTxID
	}
	return ""
}

func entryKindOrder(kind string) int {
	switch kind {
	case "position_migrated":
		return 0
	case "target_changed":
		return 1
	case "confirmations_only":
		return 2
	case "added":
		return 3
	default:
		return 4
	}
}
