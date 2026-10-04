package chain

import (
	"testing"
	"time"

	"chainresearch/internal/model"
)

// 两条研究线的夹具：共有 G->S1，LA 沿 A 分叉延伸并确认 A2，
// LB 沿 B 分叉延伸；txX 同时出现在 A2 与 B2（同一交易跨分叉）。
func lineFixture() ([]model.Block, *model.ResearchLine, *model.ResearchLine) {
	tx := func(id string, ins ...string) model.Transaction {
		t := model.Transaction{TxID: id, Outputs: []model.TxOutput{{Value: 1}}}
		for _, in := range ins {
			t.Inputs = append(t.Inputs, model.TxInput{TxID: in, Vout: 0})
		}
		return t
	}
	blocks := []model.Block{
		{Hash: "G", PrevHash: "", Work: 100, Txs: []model.Transaction{tx("coin-G")}},
		{Hash: "S1", PrevHash: "G", Work: 10, Txs: []model.Transaction{tx("coin-S1", "coin-G")}},
		{Hash: "A2", PrevHash: "S1", Work: 10, Txs: []model.Transaction{tx("txX", "coin-S1"), tx("txA2", "txX")}},
		{Hash: "A3", PrevHash: "A2", Work: 10, Txs: []model.Transaction{tx("txA3", "txA2")}},
		{Hash: "B2", PrevHash: "S1", Work: 10, Txs: []model.Transaction{tx("txX", "coin-S1")}},
		{Hash: "B3", PrevHash: "B2", Work: 10, Txs: []model.Transaction{tx("txB3", "txX")}},
	}
	now := time.Now().UTC().Format(time.RFC3339)
	la := &model.ResearchLine{
		ID: "LA", Name: "研究线 A", CreatedAt: now,
		Blocks: []string{"G", "S1", "A2", "A3"},
		Checkpoints: []model.Checkpoint{
			{Hash: "S1", ConfirmedAt: now},
			{Hash: "A2", ConfirmedAt: now},
		},
	}
	lb := &model.ResearchLine{
		ID: "LB", Name: "研究线 B", CreatedAt: now,
		Blocks: []string{"G", "S1", "B2", "B3"},
		Checkpoints: []model.Checkpoint{
			{Hash: "S1", ConfirmedAt: now},
		},
	}
	return blocks, la, lb
}

func TestCompareLinesSharedCompatibleConflicts(t *testing.T) {
	blocks, la, lb := lineFixture()
	cmp := CompareLines(la, lb, blocks, "C1")

	// 共有事实按实际区块 Hash 判断。
	if got := joinHashes(cmp.Shared); got != "G,S1" {
		t.Fatalf("共有事实应为 G,S1，实际 %s", got)
	}
	// A2/A3 是 LB 检查点（S1）之后的兼容延伸；B2/B3 跨越 LA 的
	// 确认检查点 A2，属于冲突而不是被自动撤销。
	if got := joinHashes(cmp.CompatibleA); got != "A2,A3" {
		t.Fatalf("A 线兼容后续资料应为 A2,A3，实际 %s", got)
	}
	if len(cmp.CompatibleB) != 0 {
		t.Fatalf("B 线不应有兼容后续资料，实际 %v", cmp.CompatibleB)
	}
	if got := joinHashes(cmp.OnlyB); got != "B2,B3" {
		t.Fatalf("B 线独有应为 B2,B3，实际 %s", got)
	}
	if len(cmp.Conflicts) != 2 {
		t.Fatalf("应有 2 个跨确认边界冲突，实际 %v", cmp.Conflicts)
	}
	for _, c := range cmp.Conflicts {
		if c.InLine != "LB" || c.CrossesLine != "LA" || c.Checkpoint != "A2" {
			t.Fatalf("冲突应标记为 B 线区块跨越 LA 的检查点 A2，实际 %+v", c)
		}
	}
	// 同一交易 txX 在两条线的不同分叉里分别说明。
	var note *model.TxForkNote
	for i := range cmp.TxNotes {
		if cmp.TxNotes[i].TxID == "txX" {
			note = &cmp.TxNotes[i]
		}
	}
	if note == nil {
		t.Fatalf("txX 应出现在跨分叉说明中: %+v", cmp.TxNotes)
	}
	if joinHashes(note.BlocksInA) != "A2" || joinHashes(note.BlocksInB) != "B2" {
		t.Fatalf("txX 应分别位于 A2 与 B2，实际 %+v", note)
	}
}

func TestCompareLinesBlockIdentityByHash(t *testing.T) {
	// 两个区块高度相同、交易同名，但 Hash 不同：必须视为不同区块，
	// 不能因高度或交易名相同而合并归属。
	tx := model.Transaction{TxID: "txSame"}
	blocks := []model.Block{
		{Hash: "G", PrevHash: "", Txs: []model.Transaction{tx}},
		{Hash: "P", PrevHash: "G", Txs: []model.Transaction{tx}},
		{Hash: "Q", PrevHash: "G", Txs: []model.Transaction{tx}},
	}
	la := &model.ResearchLine{ID: "LA", Blocks: []string{"G", "P"}}
	lb := &model.ResearchLine{ID: "LB", Blocks: []string{"G", "Q"}}
	cmp := CompareLines(la, lb, blocks, "C1")
	if joinHashes(cmp.Shared) != "G" {
		t.Fatalf("同高度不同 Hash 的区块不能算共有，实际 %v", cmp.Shared)
	}
	if joinHashes(cmp.OnlyA) != "P" || joinHashes(cmp.OnlyB) != "Q" {
		t.Fatalf("P 与 Q 应分别独有，实际 %v / %v", cmp.OnlyA, cmp.OnlyB)
	}
	// 同名交易 txSame 在两个分叉的出现分别说明。
	if len(cmp.TxNotes) != 1 || cmp.TxNotes[0].TxID != "txSame" {
		t.Fatalf("txSame 应有跨分叉说明，实际 %+v", cmp.TxNotes)
	}
}

func TestMergeLinesAdoptionsAndDeferred(t *testing.T) {
	blocks, la, lb := lineFixture()
	cmp := CompareLines(la, lb, blocks, "C1")
	merged, err := MergeLines(la, lb, cmp, MergeSpec{
		Name: "合并线", KeepConfirmation: "a", Accept: []string{"A3"},
	}, "LM")
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	// A2 是 LA 独有但未选中：只接纳被选中的兼容资料。
	if got := joinHashes(merged.Blocks); got != "A3,G,S1" {
		t.Fatalf("合并线区块应为 A3,G,S1，实际 %s", got)
	}
	// 每个采用决定都记录来源比较与来源线。
	adopt := map[string]model.Adoption{}
	for _, a := range merged.Adoptions {
		adopt[a.Hash] = a
		if a.ComparisonID != "C1" {
			t.Fatalf("采用决定应记录来源比较 C1，实际 %+v", a)
		}
	}
	if adopt["G"].SourceLine != "both" || adopt["A3"].SourceLine != "LA" {
		t.Fatalf("采用来源应为 both/LA，实际 %+v", adopt)
	}
	// 保留 a 方确认：只有进入新线的检查点被保留（A2 未采用则不带入）。
	if len(merged.Checkpoints) != 1 || merged.Checkpoints[0].Hash != "S1" {
		t.Fatalf("只应保留 S1 检查点，实际 %+v", merged.Checkpoints)
	}
	// 冲突部分暂缓而非判为无效；原有两条线保持不变。
	if len(merged.Deferred) != 2 {
		t.Fatalf("B2/B3 应记入暂缓，实际 %+v", merged.Deferred)
	}
	if len(la.Blocks) != 4 || len(la.Checkpoints) != 2 || len(lb.Blocks) != 4 {
		t.Fatalf("原有研究线及其确认依据应保持不变")
	}
	// 接纳冲突区块必须被拒绝。
	if _, err := MergeLines(la, lb, cmp, MergeSpec{
		Name: "x", KeepConfirmation: "a", Accept: []string{"B2"},
	}, "LX"); err == nil {
		t.Fatalf("接纳跨越确认边界的冲突区块应被拒绝")
	}
}

func TestMergePartialAcceptKeepsPendingGap(t *testing.T) {
	blocks, la, lb := lineFixture()
	cmp := CompareLines(la, lb, blocks, "C1")
	// 只接纳 A3 而不接纳中间区块 A2：缺口保留待补充，
	// 不拼接出从未存在的链。
	merged, err := MergeLines(la, lb, cmp, MergeSpec{
		Name: "部分接纳", KeepConfirmation: "a", Accept: []string{"A3"},
	}, "LM")
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	snap := BuildLineSnapshot(merged, blocks, 1, "", nil)
	var gap *model.PendingGap
	for i := range snap.Line.PendingGaps {
		if snap.Line.PendingGaps[i].Hash == "A3" {
			gap = &snap.Line.PendingGaps[i]
		}
	}
	if gap == nil || joinHashes(gap.MissingParents) != "A2" {
		t.Fatalf("A3 应标记缺少父区块 A2 的待补充范围，实际 %+v", snap.Line.PendingGaps)
	}
	// 缺口存在时 A3 不与 S1 拼成一条链：A3 是独立链根（高度 0）。
	for _, b := range snap.Blocks {
		if b.Hash == "A3" && b.Height != 0 {
			t.Fatalf("缺少中间资料时 A3 不应被拼接到 S1 之后，高度 %d", b.Height)
		}
	}
	// 迟到的共同祖先材料到达后，缺口自动消失。
	merged.Blocks = append(merged.Blocks, "A2")
	merged.Adoptions = append(merged.Adoptions, model.Adoption{
		Hash: "A2", ComparisonID: "C1", SourceLine: "LA",
	})
	snap2 := BuildLineSnapshot(merged, blocks, 2, "迟到资料", nil)
	if len(snap2.Line.PendingGaps) != 0 {
		t.Fatalf("迟到资料到达后待补充范围应消失，实际 %+v", snap2.Line.PendingGaps)
	}
	for _, b := range snap2.Blocks {
		if b.Hash == "A3" && b.Height != 3 {
			t.Fatalf("补齐后 A3 高度应为 3，实际 %d", b.Height)
		}
	}
}

func TestLineSnapshotIndependentSourcesAfterWithdraw(t *testing.T) {
	blocks, la, _ := lineFixture()
	// txA2 引用 txX；txX 只出现在 A2。撤回 A2 前引用有独立来源。
	snap := BuildLineSnapshot(la, blocks, 1, "", nil)
	var refBefore *model.TxRef
	for i := range snap.TxRefs {
		if snap.TxRefs[i].FromTxID == "txA2" && snap.TxRefs[i].ToTxID == "txX" {
			refBefore = &snap.TxRefs[i]
		}
	}
	if refBefore == nil || refBefore.IndependentSources != 1 {
		t.Fatalf("撤回前 txA2->txX 应有 1 个独立来源，实际 %+v", refBefore)
	}
	// 撤回未确认资料 A2 后，txA3->txA2 失去独立来源。
	la.Blocks = []string{"G", "S1", "A3"}
	la.Checkpoints = la.Checkpoints[:1]
	la.Withdrawals = append(la.Withdrawals, model.Withdrawal{Hash: "A2"})
	snap2 := BuildLineSnapshot(la, blocks, 2, "撤回 A2", nil)
	found := false
	for _, r := range snap2.TxRefs {
		if r.FromTxID == "txA3" && r.ToTxID == "txA2" {
			found = true
			if r.Resolved || r.IndependentSources != 0 {
				t.Fatalf("撤回后 txA3->txA2 应无独立来源，实际 %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("撤回后仍应保留 txA3->txA2 的未解析引用记录")
	}
	if len(snap2.Line.Withdrawals) != 1 || snap2.Line.Withdrawals[0].Hash != "A2" {
		t.Fatalf("撤回记录应随快照返回，实际 %+v", snap2.Line.Withdrawals)
	}
}

func joinHashes(hashes []string) string {
	out := ""
	for i, h := range hashes {
		if i > 0 {
			out += ","
		}
		out += h
	}
	return out
}
