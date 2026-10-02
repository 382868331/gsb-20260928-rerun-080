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
	snap1 := BuildSnapshot(old, 1, "旧链", nil, nil)
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
	snap2 := BuildSnapshot(blocks, 2, "分叉切换", nil, nil)
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
	snap := BuildSnapshot(sample.Blocks(), 2, "", nil, nil)
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
	snap := BuildSnapshot(sample.Blocks(), 2, "", nil, nil)
	var found *model.TxRef
	for i := range snap.TxRefs {
		r := &snap.TxRefs[i]
		if r.FromTxID == "txB4" && r.ToTxID == "txB3" {
			found = r
		}
	}
	if found == nil || !found.Resolved {
		t.Fatalf("txB4 应引用已存在的 txB3: %+v", found)
	}
}

func TestTxRefsCarryBlockBranchAndValidity(t *testing.T) {
	snap := BuildSnapshot(sample.Blocks(), 2, "", nil, nil)
	refsByFrom := map[string][]model.TxRef{}
	for _, r := range snap.TxRefs {
		refsByFrom[r.FromTxID] = append(refsByFrom[r.FromTxID], r)
	}

	// txA 在 A2（旧分叉）与 B2（主链）各出现一次，应各产生一条引用，
	// 各自标注所在区块、分支与当前有效性。
	txARefs := refsByFrom["txA"]
	if len(txARefs) != 2 {
		t.Fatalf("txA 应在两个位置各有一条引用，实际 %d: %+v", len(txARefs), txARefs)
	}
	var forkRef, mainRef *model.TxRef
	for i := range txARefs {
		r := &txARefs[i]
		if r.ToTxID != "coin-G" {
			t.Fatalf("txA 引用目标应为 coin-G: %+v", r)
		}
		if !r.Resolved || r.ToBlockHash != "G" || !r.ToCanonical {
			t.Fatalf("coin-G 应在主链解析: %+v", r)
		}
		switch r.FromBlockHash {
		case "A2":
			forkRef = r
		case "B2":
			mainRef = r
		}
	}
	if forkRef == nil || mainRef == nil {
		t.Fatalf("txA 引用应分别来自 A2 与 B2: %+v", txARefs)
	}
	// 位于旧分叉的引用不得被认作当前有效。
	if forkRef.FromCanonical || forkRef.Active {
		t.Fatalf("旧分叉上的引用不应当前有效: %+v", *forkRef)
	}
	if forkRef.FromBranchID == "main" || forkRef.FromConfirmations != 0 {
		t.Fatalf("旧分叉引用应带分叉视角且 0 确认: %+v", *forkRef)
	}
	if !mainRef.FromCanonical || !mainRef.Active || mainRef.FromBranchID != "main" {
		t.Fatalf("主链上的引用应当前有效: %+v", *mainRef)
	}
	if mainRef.FromConfirmations != 3 {
		t.Fatalf("B2 上 txA 引用应 3 确认: %+v", *mainRef)
	}

	// 引用目标只存在于旧分叉时，不得提前认作当前有效。
	var txA3Ref *model.TxRef
	for i := range snap.TxRefs {
		if snap.TxRefs[i].FromTxID == "txA3" {
			txA3Ref = &snap.TxRefs[i]
		}
	}
	if txA3Ref == nil {
		t.Fatal("txA3 应有一条引用")
	}
	if !txA3Ref.Resolved || txA3Ref.ToCanonical || txA3Ref.Active {
		t.Fatalf("txA3 引用目标只在旧分叉，应解析但非当前有效: %+v", *txA3Ref)
	}
	if txA3Ref.ToBlockHash != "A2" || txA3Ref.ToBranchID == "main" {
		t.Fatalf("txA3 引用目标应标注旧分叉位置: %+v", *txA3Ref)
	}

	// 重复构建（等价于重复导入重放）不应产生重复引用。
	snapAgain := BuildSnapshot(sample.Blocks(), 3, "", nil, nil)
	if len(snapAgain.TxRefs) != len(snap.TxRefs) {
		t.Fatalf("重复构建引用数应一致: %d != %d", len(snapAgain.TxRefs), len(snap.TxRefs))
	}
	seen := map[string]int{}
	for _, r := range snapAgain.TxRefs {
		seen[r.FromTxID+"@"+r.FromBlockHash+"->"+r.ToTxID]++
	}
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("引用 %s 出现 %d 次，应去重", k, n)
		}
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
