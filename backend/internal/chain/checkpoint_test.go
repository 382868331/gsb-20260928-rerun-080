package chain

import (
	"testing"

	"chainresearch/internal/model"
)

// 检查点测试数据集：
// G -> A1 -> A2 -> A3（主链，累计工作量 130）
//
//	\-> B1 -> B2 -> B3 -> B4（迟到长分叉，累计工作量 180，越过检查点）
//
// txX 在 A2（确认后属确认范围），txY 在 A3 引用 txX（跨边界引用），
// txX 在 B2 中重复出现，txB2 在 B2 引用 txX（冲突范围内引用确认范围目标）。
func checkpointBlocks() []model.Block {
	coinG := model.Transaction{TxID: "coin-G", Outputs: []model.TxOutput{{Value: 100, To: "addr"}}}
	txX := model.Transaction{TxID: "txX", Inputs: []model.TxInput{{TxID: "coin-G", Vout: 0}}, Outputs: []model.TxOutput{{Value: 50, To: "addr"}}}
	txY := model.Transaction{TxID: "txY", Inputs: []model.TxInput{{TxID: "txX", Vout: 0}}, Outputs: []model.TxOutput{{Value: 20, To: "addr"}}}
	txB1 := model.Transaction{TxID: "txB1", Inputs: []model.TxInput{{TxID: "coin-G", Vout: 0}}, Outputs: []model.TxOutput{{Value: 60, To: "addr"}}}
	txB2 := model.Transaction{TxID: "txB2", Inputs: []model.TxInput{{TxID: "txX", Vout: 0}}, Outputs: []model.TxOutput{{Value: 10, To: "addr"}}}
	return []model.Block{
		{Hash: "G", PrevHash: "", Work: 100, Txs: []model.Transaction{coinG}},
		{Hash: "A1", PrevHash: "G", Work: 10},
		{Hash: "A2", PrevHash: "A1", Work: 10, Txs: []model.Transaction{txX}},
		{Hash: "A3", PrevHash: "A2", Work: 10, Txs: []model.Transaction{txY}},
		{Hash: "B1", PrevHash: "G", Work: 20, Txs: []model.Transaction{txB1}},
		{Hash: "B2", PrevHash: "B1", Work: 20, Txs: []model.Transaction{txX, txB2}},
		{Hash: "B3", PrevHash: "B2", Work: 20},
		{Hash: "B4", PrevHash: "B3", Work: 20},
	}
}

func testCheckpoint(hash string, height int64, chain []string) *model.Checkpoint {
	return &model.Checkpoint{
		BlockHash:   hash,
		Height:      height,
		ConfirmedAt: "2026-09-29T00:00:00Z",
		Basis: model.CheckpointBasis{
			Version: 1, TipHash: "A3", TipHeight: 3, Chain: chain,
		},
	}
}

func blockRange(snap model.VersionSnapshot, hash string) string {
	for _, b := range snap.Blocks {
		if b.Hash == hash {
			return b.Range
		}
	}
	return ""
}

func refScope(snap model.VersionSnapshot, from, to string) string {
	for _, r := range snap.TxRefs {
		if r.FromTxID == from && r.ToTxID == to && r.Scope != "" {
			return r.Scope
		}
	}
	return ""
}

func TestLateLongForkCrossingCheckpointKeepsConflict(t *testing.T) {
	blocks := checkpointBlocks()
	cp := testCheckpoint("A2", 2, []string{"G", "A1", "A2"})

	// 无检查点时：B 链工作量更高，应成为主链。
	plain := BuildSnapshot(blocks, 1, "")
	if plain.TipHash != "B4" {
		t.Fatalf("无检查点时主链应为 B4，实际 %s", plain.TipHash)
	}

	// 确认 A2 后，迟到长分叉越过检查点：不自动撤销确认，主链钉住。
	snap := BuildSnapshotWithCheckpoint(blocks, 2, "迟到长分叉", cp, nil)
	if snap.TipHash != "A3" {
		t.Fatalf("冲突时主链应钉在含检查点的链 A3，实际 %s", snap.TipHash)
	}
	if snap.Checkpoint == nil || snap.Checkpoint.Status != "conflict" {
		t.Fatalf("检查点状态应为 conflict: %+v", snap.Checkpoint)
	}
	c := snap.Checkpoint.Conflict
	if c == nil || c.RivalTipHash != "B4" || c.RivalTipHeight != 4 || c.DivergeHeight != 0 {
		t.Fatalf("冲突信息异常: %+v", c)
	}
	// 冲突的引用差异被保留。
	if c.RefChanges == nil || len(c.RefChanges.Entries) == 0 {
		t.Fatalf("应保留冲突两种视角的引用差异: %+v", c.RefChanges)
	}

	// 范围划分：确认 / 暂定 / 冲突。
	want := map[string]string{
		"G": model.RangeConfirmed, "A1": model.RangeConfirmed, "A2": model.RangeConfirmed,
		"A3": model.RangeTentative,
		"B1": model.RangeConflict, "B2": model.RangeConflict, "B3": model.RangeConflict, "B4": model.RangeConflict,
	}
	for h, r := range want {
		if got := blockRange(snap, h); got != r {
			t.Fatalf("区块 %s 范围应为 %s，实际 %s", h, r, got)
		}
	}

	// 引用分别说明所属范围。
	if s := refScope(snap, "txX", "coin-G"); s != model.RangeConfirmed {
		t.Fatalf("txX 引用应在确认范围，实际 %q", s)
	}
	if s := refScope(snap, "txY", "txX"); s != model.RangeTentative {
		t.Fatalf("txY 引用应在暂定范围，实际 %q", s)
	}
	if s := refScope(snap, "txB1", "coin-G"); s != model.RangeConflict {
		t.Fatalf("txB1 引用应在冲突范围，实际 %q", s)
	}

	// 目标交易跨边界：txX 在 B2 重复出现，但目标仍解析到确认范围的 A2；
	// 冲突范围内的 txB2 引用确认范围的 txX，整条引用按冲突范围对待。
	for _, r := range snap.TxRefs {
		if r.FromTxID == "txB2" {
			if r.ToBlockHash != "A2" || !r.ToCanonical {
				t.Fatalf("txB2 的目标 txX 应解析到确认范围 A2: %+v", r)
			}
			if r.Scope != model.RangeConflict {
				t.Fatalf("txB2 引用应在冲突范围，实际 %q", r.Scope)
			}
		}
	}
}

func TestForkAboveCheckpointKeepsUpdating(t *testing.T) {
	blocks := []model.Block{
		{Hash: "G", PrevHash: "", Work: 100},
		{Hash: "A1", PrevHash: "G", Work: 10},
		{Hash: "A2", PrevHash: "A1", Work: 10},
		{Hash: "A3", PrevHash: "A2", Work: 10},
		{Hash: "C1", PrevHash: "A2", Work: 50},
	}
	cp := testCheckpoint("A1", 1, []string{"G", "A1"})
	snap := BuildSnapshotWithCheckpoint(blocks, 2, "检查点之后的分叉", cp, nil)
	// 分叉点在检查点之后：主链正常切换，不产生冲突。
	if snap.TipHash != "C1" {
		t.Fatalf("检查点之后的更重分叉应正常成为主链，实际 %s", snap.TipHash)
	}
	if snap.Checkpoint == nil || snap.Checkpoint.Status != "ok" || snap.Checkpoint.Conflict != nil {
		t.Fatalf("不应产生冲突: %+v", snap.Checkpoint)
	}
	if r := blockRange(snap, "A3"); r != model.RangeTentative {
		t.Fatalf("检查点之后的旧分叉块应为暂定范围，实际 %q", r)
	}
	if r := blockRange(snap, "A1"); r != model.RangeConfirmed {
		t.Fatalf("A1 应在确认范围，实际 %q", r)
	}
}

func TestRefScopeMigratesWhenCheckpointAdvances(t *testing.T) {
	blocks := checkpointBlocks()[:4] // G,A1,A2,A3
	// 检查点在 A2：txY（A3 中）引用 txX 属暂定范围。
	cp1 := testCheckpoint("A2", 2, []string{"G", "A1", "A2"})
	snap1 := BuildSnapshotWithCheckpoint(blocks, 1, "", cp1, nil)
	if s := refScope(snap1, "txY", "txX"); s != model.RangeTentative {
		t.Fatalf("检查点 A2 时 txY 引用应在暂定范围，实际 %q", s)
	}
	// 检查点推进到 A3：同一引用随边界迁移进入确认范围。
	cp2 := testCheckpoint("A3", 3, []string{"G", "A1", "A2", "A3"})
	snap2 := BuildSnapshotWithCheckpoint(blocks, 2, "", cp2, nil)
	if s := refScope(snap2, "txY", "txX"); s != model.RangeConfirmed {
		t.Fatalf("检查点 A3 时 txY 引用应迁入确认范围，实际 %q", s)
	}
}

func TestResearchLineResolvesConflictWithoutRevoking(t *testing.T) {
	blocks := checkpointBlocks()
	cp := testCheckpoint("A2", 2, []string{"G", "A1", "A2"})
	lines := []model.ResearchLine{{ID: "rl-B4", Label: "独立研究", TipHash: "B4", TipHeight: 4}}
	snap := BuildSnapshotWithCheckpoint(blocks, 3, "", cp, lines)
	// 竞争链登记为独立研究线后：不再是待决冲突，但主链仍钉住、确认不撤销。
	if snap.TipHash != "A3" {
		t.Fatalf("登记研究线后主链仍应钉在 A3，实际 %s", snap.TipHash)
	}
	if snap.Checkpoint == nil || snap.Checkpoint.Status != "ok" || snap.Checkpoint.Conflict != nil {
		t.Fatalf("研究线登记后不应再有待决冲突: %+v", snap.Checkpoint)
	}
	if len(snap.ResearchLines) != 1 || snap.ResearchLines[0].TipHash != "B4" {
		t.Fatalf("快照应携带独立研究线: %+v", snap.ResearchLines)
	}
	// B 链区块仍标为冲突范围（它们确实越过检查点），供研究者核对。
	if r := blockRange(snap, "B4"); r != model.RangeConflict {
		t.Fatalf("B4 仍应在冲突范围，实际 %q", r)
	}
}
