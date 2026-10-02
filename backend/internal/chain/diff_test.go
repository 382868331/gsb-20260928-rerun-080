package chain

import (
	"testing"

	"chainresearch/internal/model"
	"chainresearch/internal/sample"
)

func oldChainBlocks() []model.Block {
	var out []model.Block
	for _, b := range sample.Blocks() {
		if b.Hash == "G" || b.Hash[0] == 'A' {
			out = append(out, b)
		}
	}
	return out
}

func entriesByKind(diff model.RefDiff, kind string) []model.RefDiffEntry {
	var out []model.RefDiffEntry
	for _, e := range diff.Entries {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestDiffRefsAcrossForkSwitch(t *testing.T) {
	snap1 := BuildSnapshot(oldChainBlocks(), 1, "旧链", nil, nil)
	snap2 := BuildSnapshot(sample.Blocks(), 2, "分叉切换", nil, nil)
	diff := DiffRefs(snap1, snap2)
	if diff.FromVersion != 1 || diff.ToVersion != 2 {
		t.Fatalf("差异版本号错误: %+v", diff)
	}

	// txA 的引用从旧链 A2 迁移到新主链 B2：位置迁移。
	migrated := entriesByKind(diff, "position_migrated")
	foundMigrate := false
	for _, e := range migrated {
		if e.Before.FromTxID == "txA" && e.Before.FromBlockHash == "A2" &&
			e.After.FromBlockHash == "B2" && e.After.Active {
			foundMigrate = true
		}
	}
	if !foundMigrate {
		t.Fatalf("txA 引用应从 A2 迁移到 B2: %+v", migrated)
	}

	// txA2/txA3 的引用目标随旧链撤回，目标链视角变化：目标变化。
	targetChanged := entriesByKind(diff, "target_changed")
	changedFrom := map[string]bool{}
	for _, e := range targetChanged {
		changedFrom[e.Before.FromTxID] = true
		if e.Before.ToCanonical && !e.After.ToCanonical {
			// 目标从主链变为旧分叉，符合预期。
		}
	}
	for _, id := range []string{"txA2", "txA3"} {
		if !changedFrom[id] {
			t.Fatalf("%s 的引用目标应随分叉切换变化: %+v", id, targetChanged)
		}
	}

	// 新链上新增的引用（txB3、txB4）应记为 added。
	added := entriesByKind(diff, "added")
	addedFrom := map[string]bool{}
	for _, e := range added {
		addedFrom[e.After.FromTxID] = true
	}
	for _, id := range []string{"txB3", "txB4"} {
		if !addedFrom[id] {
			t.Fatalf("%s 的引用应为新增: %+v", id, added)
		}
	}
}

func TestDiffRefsConfirmationsOnly(t *testing.T) {
	blocks := oldChainBlocks()
	snap1 := BuildSnapshot(blocks, 1, "", nil, nil)
	// 在同一主链上延长一个区块：引用位置与目标不变，仅确认数变化。
	tip := model.Block{
		Hash: "A4", PrevHash: "A3", Work: 10,
		Txs: []model.Transaction{{TxID: "txA4", Outputs: []model.TxOutput{{Value: 5}}}},
	}
	snap2 := BuildSnapshot(append(append([]model.Block{}, blocks...), tip), 2, "", nil, nil)
	diff := DiffRefs(snap1, snap2)

	confOnly := entriesByKind(diff, "confirmations_only")
	if len(confOnly) == 0 {
		t.Fatal("延长主链后应存在仅确认数变化的引用")
	}
	for _, e := range confOnly {
		if e.Before.FromBlockHash != e.After.FromBlockHash || e.Before.ToBlockHash != e.After.ToBlockHash {
			t.Fatalf("仅确认数变化的引用位置不应变: %+v", e)
		}
		if e.After.FromConfirmations != e.Before.FromConfirmations+1 {
			t.Fatalf("确认数应 +1: %+v", e)
		}
	}
	// 不应误报位置迁移或目标变化。
	if n := len(entriesByKind(diff, "position_migrated")); n != 0 {
		t.Fatalf("同链延长不应有位置迁移: %d", n)
	}
	if n := len(entriesByKind(diff, "target_changed")); n != 0 {
		t.Fatalf("同链延长不应有目标变化: %d", n)
	}
}

func TestDiffRefsIdenticalVersionsEmpty(t *testing.T) {
	snap := BuildSnapshot(sample.Blocks(), 1, "", nil, nil)
	diff := DiffRefs(snap, snap)
	if len(diff.Entries) != 0 {
		t.Fatalf("相同版本差异应为空: %+v", diff.Entries)
	}
}
