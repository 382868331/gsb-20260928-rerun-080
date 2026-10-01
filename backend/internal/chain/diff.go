package chain

import (
	"sort"
	"strconv"

	"chainresearch/internal/model"
)

// DiffRefs 计算两个索引版本之间引用关系的差异，用于观察
// 分叉切换前后引用的变化。变化按四类区分：
//   - moved：同一引用（from->to:vout）的花费方位置迁移；
//   - retargeted：同一花费位置的引用目标变化；
//   - confirmations：位置与目标不变，仅确认数变化；
//   - added / removed：引用出现或消失。
func DiffRefs(from, to model.VersionSnapshot) model.RefDiff {
	diff := model.RefDiff{
		FromVersion: from.Version,
		ToVersion:   to.Version,
		Changes:     []model.RefChange{},
	}

	keyOf := func(r model.TxRef) string {
		return r.FromTxID + "->" + r.ToTxID + ":" + strconv.Itoa(r.Vout)
	}
	beforeByKey := map[string][]model.TxRef{}
	for _, r := range from.TxRefs {
		beforeByKey[keyOf(r)] = append(beforeByKey[keyOf(r)], r)
	}
	afterByKey := map[string][]model.TxRef{}
	for _, r := range to.TxRefs {
		afterByKey[keyOf(r)] = append(afterByKey[keyOf(r)], r)
	}

	var removed, added []model.TxRef
	keys := map[string]bool{}
	for k := range beforeByKey {
		keys[k] = true
	}
	for k := range afterByKey {
		keys[k] = true
	}
	sortedKeys := make([]string, 0, len(keys))
	for k := range keys {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	for _, k := range sortedKeys {
		befores := beforeByKey[k]
		afters := afterByKey[k]
		matchedAfter := map[int]bool{}
		for _, b := range befores {
			found := -1
			for i, a := range afters {
				if matchedAfter[i] {
					continue
				}
				if a.FromBlockHash == b.FromBlockHash {
					found = i
					break
				}
			}
			if found >= 0 {
				matchedAfter[found] = true
				a := afters[found]
				if a.Confirmations != b.Confirmations || a.Valid != b.Valid {
					diff.Changes = append(diff.Changes, change("confirmations", b, a))
				}
				continue
			}
			// 同键但花费位置不同：位置迁移。配对第一个未匹配的位置。
			moved := -1
			for i := range afters {
				if !matchedAfter[i] {
					moved = i
					break
				}
			}
			if moved >= 0 {
				matchedAfter[moved] = true
				diff.Changes = append(diff.Changes, change("moved", b, afters[moved]))
			} else {
				removed = append(removed, b)
			}
		}
		for i, a := range afters {
			if !matchedAfter[i] {
				added = append(added, a)
			}
		}
	}

	// 引用目标变化：同一花费位置（from_txid@区块:vout）在旧版本指向
	// 一个目标、新版本指向另一个目标，配对为 retargeted 而非增删。
	posKey := func(r model.TxRef) string {
		return r.FromTxID + "@" + r.FromBlockHash + ":" + strconv.Itoa(r.Vout)
	}
	addedByPos := map[string][]int{}
	for i, a := range added {
		addedByPos[posKey(a)] = append(addedByPos[posKey(a)], i)
	}
	usedAdded := map[int]bool{}
	var stillRemoved []model.TxRef
	for _, b := range removed {
		idxs := addedByPos[posKey(b)]
		picked := -1
		for _, i := range idxs {
			if !usedAdded[i] {
				picked = i
				break
			}
		}
		if picked >= 0 {
			usedAdded[picked] = true
			diff.Changes = append(diff.Changes, change("retargeted", b, added[picked]))
		} else {
			stillRemoved = append(stillRemoved, b)
		}
	}
	for _, b := range stillRemoved {
		diff.Changes = append(diff.Changes, change("removed", b, model.TxRef{}))
	}
	for i, a := range added {
		if !usedAdded[i] {
			diff.Changes = append(diff.Changes, change("added", model.TxRef{}, a))
		}
	}

	sort.SliceStable(diff.Changes, func(i, j int) bool {
		if diff.Changes[i].Kind != diff.Changes[j].Kind {
			return diff.Changes[i].Kind < diff.Changes[j].Kind
		}
		if diff.Changes[i].FromTxID != diff.Changes[j].FromTxID {
			return diff.Changes[i].FromTxID < diff.Changes[j].FromTxID
		}
		return diff.Changes[i].ToTxID < diff.Changes[j].ToTxID
	})
	return diff
}

func change(kind string, before, after model.TxRef) model.RefChange {
	c := model.RefChange{Kind: kind}
	if kind != "added" {
		b := before
		c.Before = &b
		c.FromTxID = before.FromTxID
		c.ToTxID = before.ToTxID
		c.Vout = before.Vout
	}
	if kind != "removed" {
		a := after
		c.After = &a
		c.FromTxID = after.FromTxID
		c.Vout = after.Vout
		if kind == "added" {
			c.ToTxID = after.ToTxID
		}
	}
	if kind == "retargeted" {
		c.ToTxID = before.ToTxID + " -> " + after.ToTxID
	}
	return c
}
