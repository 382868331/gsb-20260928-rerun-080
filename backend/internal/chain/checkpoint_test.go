package chain

import (
	"testing"

	"chainresearch/internal/model"
	"chainresearch/internal/sample"
)

func oldChainOnly() []model.Block {
	var out []model.Block
	for _, b := range sample.Blocks() {
		if b.Hash == "G" || b.Hash[0] == 'A' {
			out = append(out, b)
		}
	}
	return out
}

func cp(hash string) []model.Checkpoint {
	return []model.Checkpoint{{Hash: hash, ConfirmedAt: "2026-09-10T00:00:00Z"}}
}

func scopeMap(snap model.VersionSnapshot) map[string]string {
	m := map[string]string{}
	for _, b := range snap.Blocks {
		m[b.Hash] = b.Scope
	}
	return m
}

// 迟到长分叉越过检查点：即使累计工作量更高，也不自动撤销已确认范围，
// 只记为冲突；检查点前为确认范围、检查点后为暂定范围。
func TestCheckpointBlocksLateLongFork(t *testing.T) {
	blocks := sample.Blocks()
	checkpoints := cp("A2")

	snap := BuildSnapshot(blocks, 3, "迟到长分叉", checkpoints, nil)
	if snap.TipHash != "A3" {
		t.Fatalf("有检查点时主链端点应仍为 A3，实际 %s", snap.TipHash)
	}
	if snap.Checkpoint == nil || snap.Checkpoint.Hash != "A2" || snap.Checkpoint.Height != 2 {
		t.Fatalf("快照应携带检查点 A2@2: %+v", snap.Checkpoint)
	}
	scopes := scopeMap(snap)
	for _, h := range []string{"G", "A1", "A2"} {
		if scopes[h] != model.ScopeConfirmed {
			t.Fatalf("%s 应在确认范围，实际 %s", h, scopes[h])
		}
	}
	if scopes["A3"] != model.ScopeTentative {
		t.Fatalf("A3 应在暂定范围，实际 %s", scopes["A3"])
	}
	for _, h := range []string{"B1", "B2", "B3", "B4"} {
		if scopes[h] != model.ScopeConflict {
			t.Fatalf("%s 应在冲突范围，实际 %s", h, scopes[h])
		}
	}
	if len(snap.Conflicts) != 1 {
		t.Fatalf("应记录一条冲突: %+v", snap.Conflicts)
	}
	c := snap.Conflicts[0]
	if c.RivalTip != "B4" || c.RivalWork != 140 || c.Status != "pending" {
		t.Fatalf("冲突应指向 B4、链上累计工作量 140、待裁决: %+v", c)
	}

	// 研究者裁决建立独立研究后：冲突保留但状态变更，确认范围不被撤销。
	snap2 := BuildSnapshot(blocks, 4, "建立独立研究", checkpoints, map[string]string{c.ID: "independent"})
	if snap2.TipHash != "A3" {
		t.Fatalf("裁决后原确认仍不得撤销，主链端点应为 A3，实际 %s", snap2.TipHash)
	}
	if len(snap2.Conflicts) != 1 || snap2.Conflicts[0].Status != "independent" {
		t.Fatalf("冲突应标记为 independent: %+v", snap2.Conflicts)
	}
	if scopeMap(snap2)["A2"] != model.ScopeConfirmed {
		t.Fatal("裁决后 A2 仍应在确认范围")
	}
}

// 检查点之后的分叉变化可继续更新：从检查点之后分出的更高工作量链
// 正常成为主链。
func TestForkAboveCheckpointStillUpdates(t *testing.T) {
	blocks := append(oldChainOnly(),
		model.Block{Hash: "C1", PrevHash: "A2", Work: 10, Txs: []model.Transaction{{TxID: "txC1"}}},
		model.Block{Hash: "C2", PrevHash: "C1", Work: 10, Txs: []model.Transaction{{TxID: "txC2"}}},
	)
	snap := BuildSnapshot(blocks, 2, "检查点后的分叉", cp("A2"), nil)
	if snap.TipHash != "C2" {
		t.Fatalf("检查点之后的更优分叉应成为主链，实际端点 %s", snap.TipHash)
	}
	scopes := scopeMap(snap)
	if scopes["A3"] != model.ScopeReverted {
		t.Fatalf("A3 应作为普通分叉被撤回，实际 %s", scopes["A3"])
	}
	if scopes["C1"] != model.ScopeTentative || scopes["C2"] != model.ScopeTentative {
		t.Fatalf("C1/C2 应在暂定范围: %s %s", scopes["C1"], scopes["C2"])
	}
	if len(snap.Conflicts) != 0 {
		t.Fatalf("检查点之后的分叉不算冲突: %+v", snap.Conflicts)
	}
}

// 目标交易跨边界迁移：同一交易同时出现在确认范围与冲突范围时，
// 引用目标解析到确认范围的位置；只存在于冲突范围的目标按冲突范围标注。
func TestRefScopesAcrossCheckpointBoundary(t *testing.T) {
	snap := BuildSnapshot(sample.Blocks(), 3, "", cp("A2"), nil)

	var txARefFromA2, txARefFromB2 *model.TxRef
	for i := range snap.TxRefs {
		r := &snap.TxRefs[i]
		if r.FromTxID == "txA" && r.FromBlockHash == "A2" {
			txARefFromA2 = r
		}
		if r.FromTxID == "txA" && r.FromBlockHash == "B2" {
			txARefFromB2 = r
		}
	}
	if txARefFromA2 == nil || txARefFromB2 == nil {
		t.Fatal("txA 应在确认范围与冲突范围各有一条引用")
	}
	if txARefFromA2.FromScope != model.ScopeConfirmed || !txARefFromA2.Active {
		t.Fatalf("确认范围内的引用应当前有效: %+v", *txARefFromA2)
	}
	if txARefFromB2.FromScope != model.ScopeConflict || txARefFromB2.Active {
		t.Fatalf("冲突范围内的引用不应认作当前有效: %+v", *txARefFromB2)
	}
	// txA 的引用目标 coin-G 在主链确认范围。
	if txARefFromB2.ToScope != model.ScopeConfirmed {
		t.Fatalf("引用目标应解析到确认范围: %+v", *txARefFromB2)
	}
	// txB3 只存在于冲突范围：txB4 的引用目标应标注冲突范围。
	var txB4Ref *model.TxRef
	for i := range snap.TxRefs {
		if snap.TxRefs[i].FromTxID == "txB4" {
			txB4Ref = &snap.TxRefs[i]
		}
	}
	if txB4Ref == nil || txB4Ref.ToScope != model.ScopeConflict || txB4Ref.Active {
		t.Fatalf("目标只在冲突范围的引用不应认作当前有效: %+v", txB4Ref)
	}
	// 交易位置同样携带范围标记。
	for _, p := range snap.TxIndex {
		if p.TxID == "txA" && p.BlockHash == "A2" && p.Scope != model.ScopeConfirmed {
			t.Fatalf("txA@A2 位置应在确认范围: %+v", p)
		}
		if p.TxID == "txA" && p.BlockHash == "B2" && p.Scope != model.ScopeConflict {
			t.Fatalf("txA@B2 位置应在冲突范围: %+v", p)
		}
	}
}

// 重复区块（重放同一批数据）不产生重复引用，且范围标记稳定。
func TestCheckpointReplayKeepsScopesStable(t *testing.T) {
	snap1 := BuildSnapshot(sample.Blocks(), 3, "", cp("A2"), nil)
	snap2 := BuildSnapshot(sample.Blocks(), 4, "", cp("A2"), nil)
	if len(snap1.TxRefs) != len(snap2.TxRefs) || len(snap1.Conflicts) != len(snap2.Conflicts) {
		t.Fatal("重放后引用与冲突数量应一致")
	}
	for h, s1 := range scopeMap(snap1) {
		if scopeMap(snap2)[h] != s1 {
			t.Fatalf("重放后 %s 范围标记应稳定", h)
		}
	}
}
