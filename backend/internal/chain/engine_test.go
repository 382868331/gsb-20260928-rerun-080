package chain

import (
	"testing"

	"chainresearch/internal/model"
	"chainresearch/internal/sample"
)

func canonicalMap(snap model.VersionSnapshot) map[string]bool {
	m := map[string]bool{}
	for _, b := range snap.Blocks {
		m[b.Hash] = b.Canonical
	}
	return m
}

func TestReorgRevertsOldChainAndKeepsTrace(t *testing.T) {
	blocks := sample.Blocks()
	// 先只导入 G,A1,A2,A3：旧链为主链。
	old := make([]model.Block, 0, 4)
	for _, b := range blocks {
		if b.Hash == "G" || b.Hash[0] == 'A' {
			old = append(old, b)
		}
	}
	snap1 := BuildSnapshot(old, 1, "旧链")
	if snap1.TipHash != "A3" {
		t.Fatalf("初始主链端点应为 A3，实际 %s", snap1.TipHash)
	}
	cm := canonicalMap(snap1)
	for _, h := range []string{"G", "A1", "A2", "A3"} {
		if !cm[h] {
			t.Fatalf("版本1中 %s 应为主链", h)
		}
	}
	a3pos := findTx(snap1.TxIndex, "txA3")
	if a3pos == nil || !a3pos.Canonical || a3pos.Confirmations != 1 {
		t.Fatalf("txA3 应在主链且 1 确认: %+v", a3pos)
	}

	// 导入完整数据集：B 链工作量更高，成为新主链，旧结果被撤回。
	snap2 := BuildSnapshot(blocks, 2, "分叉切换")
	if snap2.TipHash != "B4" {
		t.Fatalf("切换后主链端点应为 B4，实际 %s", snap2.TipHash)
	}
	cm2 := canonicalMap(snap2)
	for _, h := range []string{"G", "B1", "B2", "B3", "B4"} {
		if !cm2[h] {
			t.Fatalf("版本2中 %s 应为主链", h)
		}
	}
	for _, h := range []string{"A1", "A2", "A3"} {
		if cm2[h] {
			t.Fatalf("版本2中旧链块 %s 应被撤回 canonical=false", h)
		}
	}
	// 旧分支仍可追溯：A 块保留，确认数归 0，分叉 ID 一致。
	a1, a2 := viewByHash(snap2, "A1"), viewByHash(snap2, "A2")
	if a1 == nil || a2 == nil {
		t.Fatal("旧分叉区块应保留在快照中")
	}
	if a1.Confirmations != 0 || a2.Confirmations != 0 {
		t.Fatalf("被撤回区块确认数应为 0: %d %d", a1.Confirmations, a2.Confirmations)
	}
	if a1.BranchID != a2.BranchID || a1.BranchID == "main" {
		t.Fatalf("同一旧分叉应共享分叉 ID: %s %s", a1.BranchID, a2.BranchID)
	}
	// txA3 的确认状态随主链切换一起变化。
	a3new := findTx(snap2.TxIndex, "txA3")
	if a3new == nil || a3new.Canonical || a3new.Confirmations != 0 {
		t.Fatalf("txA3 应随旧链撤回: %+v", a3new)
	}
	// 新主链上的 txB4 为 1 确认。
	b4 := findTx(snap2.TxIndex, "txB4")
	if b4 == nil || !b4.Canonical || b4.Confirmations != 1 {
		t.Fatalf("txB4 应在新主链且 1 确认: %+v", b4)
	}
}

func TestDuplicateTransactionHasMultiplePositions(t *testing.T) {
	snap := BuildSnapshot(sample.Blocks(), 2, "")
	var positions []model.TxPosition
	for _, p := range snap.TxIndex {
		if p.TxID == "txA" {
			positions = append(positions, p)
		}
	}
	if len(positions) != 2 {
		t.Fatalf("txA 应在 A2 与 B2 各有一个位置，实际 %d", len(positions))
	}
	hasCanon, hasFork := false, false
	for _, p := range positions {
		if p.BlockHash == "B2" && p.Canonical {
			hasCanon = true
		}
		if p.BlockHash == "A2" && !p.Canonical {
			hasFork = true
		}
	}
	if !hasCanon || !hasFork {
		t.Fatalf("txA 两个位置应分别为新主链/旧分叉: %+v", positions)
	}
}

func TestTxReferencesResolved(t *testing.T) {
	snap := BuildSnapshot(sample.Blocks(), 2, "")
	refs := map[string]model.TxRef{}
	for _, r := range snap.TxRefs {
		refs[r.FromTxID+"->"+r.ToTxID] = r
	}
	r, ok := refs["txB4->txB3"]
	if !ok || !r.Resolved {
		t.Fatalf("txB4 应引用已存在的 txB3: %+v", r)
	}
}

func findTx(ps []model.TxPosition, id string) *model.TxPosition {
	for i := range ps {
		if ps[i].TxID == id {
			return &ps[i]
		}
	}
	return nil
}

func viewByHash(snap model.VersionSnapshot, h string) *model.BlockView {
	for i := range snap.Blocks {
		if snap.Blocks[i].Hash == h {
			return &snap.Blocks[i]
		}
	}
	return nil
}
