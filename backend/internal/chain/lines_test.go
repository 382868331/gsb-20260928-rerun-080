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
		{Hash: "A4", PrevHash: "A3", Work: 10, Txs: []model.Transaction{tx("txA4", "txA3")}},
		{Hash: "B2", PrevHash: "S1", Work: 10, Txs: []model.Transaction{tx("txX", "coin-S1")}},
		{Hash: "B3", PrevHash: "B2", Work: 10, Txs: []model.Transaction{tx("txB3", "txX")}},
		{Hash: "B4", PrevHash: "B3", Work: 10, Txs: []model.Transaction{tx("txB4", "txB3")}},
	}
	now := time.Now().UTC().Format(time.RFC3339)
	la := &model.ResearchLine{
		ID: "LA", Name: "研究线 A", CreatedAt: now,
		Blocks: []string{"G", "S1", "A2", "A3", "A4"},
		Checkpoints: []model.Checkpoint{
			{Hash: "S1", ConfirmedAt: now},
			{Hash: "A2", ConfirmedAt: now},
		},
	}
	lb := &model.ResearchLine{
		ID: "LB", Name: "研究线 B", CreatedAt: now,
		Blocks: []string{"G", "S1", "B2", "B3", "B4"},
		Checkpoints: []model.Checkpoint{
			{Hash: "B2", ConfirmedAt: now},
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
	// A3/A4 是 LB 检查点（B2）之后与 B 线无关的正常链外资料；B3/B4 跨越 LA 的
	// 确认检查点 A2，属于冲突而不是被自动撤销。
	if len(cmp.CompatibleA) != 0 {
		t.Fatalf("A 线后续资料相对 B2 均为冲突，兼容集合应为空，实际 %v", cmp.CompatibleA)
	}
	if len(cmp.CompatibleB) != 0 {
		t.Fatalf("B 线不应有兼容后续资料，实际 %v", cmp.CompatibleB)
	}
	if got := joinHashes(cmp.OnlyB); got != "B2,B3,B4" {
		t.Fatalf("B 线独有应为 B2,B3,B4，实际 %s", got)
	}
	if len(cmp.Conflicts) != 6 {
		t.Fatalf("双向不同检查点应有 6 个跨确认边界冲突，实际 %v", cmp.Conflicts)
	}
	for _, c := range cmp.Conflicts {
		switch c.InLine {
		case "LA":
			if c.CrossesLine != "LB" || c.Checkpoint != "B2" {
				t.Fatalf("A 线冲突应标记为跨越 LB 的检查点 B2，实际 %+v", c)
			}
		case "LB":
			if c.CrossesLine != "LA" || c.Checkpoint != "A2" {
				t.Fatalf("B 线冲突应标记为跨越 LA 的检查点 A2，实际 %+v", c)
			}
		default:
			t.Fatalf("冲突来源线异常: %+v", c)
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
	merged, err := MergeLinesWithBlocks(la, lb, blocks, cmp, MergeSpec{
		Name: "合并线", KeepConfirmation: "a", Accept: []string{"A4"},
	}, "LM")
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	// 保留 A 线确认时，A 线确认范围 G,S1,A2 完整继承；A3 未接纳，
	// A4 作为缺少父块的待接纳资料保留。
	if got := joinHashes(merged.Blocks); got != "A2,A4,G,S1" {
		t.Fatalf("合并线区块应为 A2,A4,G,S1，实际 %s", got)
	}
	// 每个采用决定都记录来源比较与来源线。
	adopt := map[string]model.Adoption{}
	for _, a := range merged.Adoptions {
		adopt[a.Hash] = a
		if a.ComparisonID != "C1" {
			t.Fatalf("采用决定应记录来源比较 C1，实际 %+v", a)
		}
	}
	if adopt["G"].SourceLine != "both" || adopt["A2"].SourceLine != "LA" || adopt["A4"].SourceLine != "LA" {
		t.Fatalf("采用来源应为 both/LA，实际 %+v", adopt)
	}
	if len(merged.Checkpoints) != 2 {
		t.Fatalf("应完整保留 A 线 S1/A2 检查点，实际 %+v", merged.Checkpoints)
	}
	for _, cp := range merged.Checkpoints {
		if cp.Hash != "S1" && cp.Hash != "A2" {
			t.Fatalf("检查点应为 S1/A2，实际 %+v", cp)
		}
	}
	// 只有对方 B2/B3/B4 跨越所选 A2 的冲突暂缓；A2 自身相对 B2 的冲突标记不能暂缓甲方确认。
	if len(merged.Deferred) != 3 {
		t.Fatalf("B2/B3/B4 应记入暂缓，实际 %+v", merged.Deferred)
	}
	if len(la.Blocks) != 5 || len(la.Checkpoints) != 2 || len(lb.Blocks) != 5 {
		t.Fatalf("原有研究线及其确认依据应保持不变")
	}
	// 接纳冲突区块必须被拒绝。
	if _, err := MergeLinesWithBlocks(la, lb, blocks, cmp, MergeSpec{
		Name: "x", KeepConfirmation: "a", Accept: []string{"B2"},
	}, "LX"); err == nil {
		t.Fatalf("接纳跨越确认边界的冲突区块应被拒绝")
	}
}

func TestMergeLinesKeepsChosenConfirmationBothDirections(t *testing.T) {
	blocks, la, lb := lineFixture()
	cmp := CompareLines(la, lb, blocks, "C1")
	mergedB, err := MergeLinesWithBlocks(la, lb, blocks, cmp, MergeSpec{
		Name: "保留 B", KeepConfirmation: "b", Accept: []string{"B4"},
	}, "LMB")
	if err != nil {
		t.Fatalf("保留 B 线确认合并失败: %v", err)
	}
	if got := joinHashes(mergedB.Blocks); got != "B2,B4,G,S1" {
		t.Fatalf("B 线确认范围与待接纳 B4 应为 B2,B4,G,S1，实际 %s", got)
	}
	if len(mergedB.Checkpoints) != 1 || mergedB.Checkpoints[0].Hash != "B2" {
		t.Fatalf("应继承 B 线 B2 检查点，实际 %+v", mergedB.Checkpoints)
	}
	if len(mergedB.Deferred) != 3 {
		t.Fatalf("A2/A3/A4 应相对 B2 暂缓，实际 %+v", mergedB.Deferred)
	}
}

func TestMergePartialAcceptKeepsPendingGap(t *testing.T) {
	blocks, la, lb := lineFixture()
	cmp := CompareLines(la, lb, blocks, "C1")
	// 只接纳 A3 而不接纳中间区块 A2：缺口保留待补充，
	// 不拼接出从未存在的链。
	merged, err := MergeLinesWithBlocks(la, lb, blocks, cmp, MergeSpec{
		Name: "部分接纳", KeepConfirmation: "a", Accept: []string{"A4"},
	}, "LM")
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	snap := BuildLineSnapshot(merged, blocks, 1, "", nil)
	if len(snap.Conflicts) != 0 {
		t.Fatalf("缺少父块的待接纳资料不能记为冲突，实际 %+v", snap.Conflicts)
	}
	var gap *model.PendingGap
	for i := range snap.Line.PendingGaps {
		if snap.Line.PendingGaps[i].Hash == "A4" {
			gap = &snap.Line.PendingGaps[i]
		}
	}
	if gap == nil || joinHashes(gap.MissingParents) != "A3" || gap.SourceLine != "LA" {
		t.Fatalf("A4 应标记缺少父区块 A3 的待补充范围及 LA 出处，实际 %+v", snap.Line.PendingGaps)
	}
	// 缺口存在时 A3 不与 S1 拼成一条链：A3 是独立链根（高度 0）。
	for _, b := range snap.Blocks {
		if b.Hash == "A4" && b.Height != 0 {
			t.Fatalf("缺少中间资料时 A4 不应被拼接到 A2 之后，高度 %d", b.Height)
		}
		if b.Hash == "A4" && b.Scope != model.ScopePending {
			t.Fatalf("缺少父块的 A4 应为待补充范围，实际 %s", b.Scope)
		}
	}
	// 迟到的共同祖先材料到达后，缺口自动消失。
	merged.Blocks = append(merged.Blocks, "A3")
	merged.Adoptions = append(merged.Adoptions, model.Adoption{
		Hash: "A3", ComparisonID: "C1", SourceLine: "LA",
	})
	snap2 := BuildLineSnapshot(merged, blocks, 2, "迟到资料", nil)
	if len(snap2.Line.PendingGaps) != 0 {
		t.Fatalf("迟到资料到达后待补充范围应消失，实际 %+v", snap2.Line.PendingGaps)
	}
	for _, b := range snap2.Blocks {
		if b.Hash == "A4" && b.Height != 4 {
			t.Fatalf("补齐后 A4 高度应为 4，实际 %d", b.Height)
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

// 缺父块 gap-tip 夹具：乙方资料 T 声称父块为 P，但 P 尚未导入任何地方
// （全局缺失）。甲方确认 S1（G->S1）；T 与 S1 的关系此刻无法证明。
func missingParentFixture() ([]model.Block, *model.ResearchLine, *model.ResearchLine, model.Block) {
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
		// T 指向尚未到达的父块 P（PrevHash=P，但 P 不在 blocks 中）。
		{Hash: "T", PrevHash: "P", Work: 10, Txs: []model.Transaction{tx("txT", "coin-S1")}},
	}
	// 迟到父块：P 指向甲方已确认的 S1，因此 T 实为已确认 S1 的后续。
	lateParent := model.Block{Hash: "P", PrevHash: "S1", Work: 10, Txs: []model.Transaction{tx("txP", "coin-S1")}}
	now := time.Now().UTC().Format(time.RFC3339)
	la := &model.ResearchLine{
		ID: "LA", Name: "甲方", CreatedAt: now,
		Blocks:      []string{"G", "S1"},
		Checkpoints: []model.Checkpoint{{Hash: "S1", ConfirmedAt: now}},
	}
	lb := &model.ResearchLine{
		ID: "LB", Name: "乙方", CreatedAt: now,
		Blocks: []string{"G", "S1", "T"},
	}
	return blocks, la, lb, lateParent
}

// 父块缺失时 gap-tip 不能被当作有证据的冲突：比较中它应归入
// 可接纳资料，合并接纳后作为有出处的待补充项保留；父块迟到并
// 补齐后重新判断为已确认检查点的正常后续。
func TestMissingParentGapTipKeptThenLateParentRejudged(t *testing.T) {
	blocks, la, lb, lateParent := missingParentFixture()
	cmp := CompareLines(la, lb, blocks, "C1")
	if len(cmp.Conflicts) != 0 {
		t.Fatalf("父块缺失时关系未知，不能记为冲突，实际 %+v", cmp.Conflicts)
	}
	if got := joinHashes(cmp.CompatibleB); got != "T" {
		t.Fatalf("缺父块的 T 应归入乙方可接纳资料，实际 %v", cmp.CompatibleB)
	}

	// 保留甲方确认并接纳 T：不应被拒绝，T 作为缺父块的待补充项保留。
	merged, err := MergeLinesWithBlocks(la, lb, blocks, cmp, MergeSpec{
		Name: "保留甲方", KeepConfirmation: "a", Accept: []string{"T"},
	}, "LM")
	if err != nil {
		t.Fatalf("缺父块的 gap-tip 应被接纳为待补充项，实际被拒绝: %v", err)
	}
	if got := joinHashes(merged.Blocks); got != "G,S1,T" {
		t.Fatalf("合并线应含 T，实际 %s", got)
	}
	if len(merged.Deferred) != 0 {
		t.Fatalf("缺父块资料不能暂缓为冲突，实际 %+v", merged.Deferred)
	}
	snap := BuildLineSnapshot(merged, blocks, 1, "合并", nil)
	var gap *model.PendingGap
	for i := range snap.Line.PendingGaps {
		if snap.Line.PendingGaps[i].Hash == "T" {
			gap = &snap.Line.PendingGaps[i]
		}
	}
	if gap == nil || joinHashes(gap.MissingParents) != "P" || gap.SourceLine != "LB" {
		t.Fatalf("T 应保留缺父块 P 的待补充状态及乙方出处，实际 %+v", snap.Line.PendingGaps)
	}
	for _, b := range snap.Blocks {
		if b.Hash == "T" && b.Scope != model.ScopePending {
			t.Fatalf("缺父块的 T 应为待补充范围，实际 %s", b.Scope)
		}
	}

	// 补交指向已确认 S1 的父块 P：缺口消失，T 重新判断为 S1 的正常后续。
	withParent := append([]model.Block{}, append(blocks, lateParent)...)
	merged.Blocks = append(merged.Blocks, "P")
	merged.Adoptions = append(merged.Adoptions, model.Adoption{
		Hash: "P", ComparisonID: "C1", SourceLine: "LB", AdoptedAt: time.Now().UTC().Format(time.RFC3339),
	})
	snap2 := BuildLineSnapshot(merged, withParent, 2, "补交父块 P", nil)
	if len(snap2.Line.PendingGaps) != 0 {
		t.Fatalf("父块到达后待补充范围应消失，实际 %+v", snap2.Line.PendingGaps)
	}
	for _, b := range snap2.Blocks {
		if b.Hash == "T" {
			if b.Scope != model.ScopeTentative {
				t.Fatalf("补齐后 T 应为检查点 S1 之后的暂定后续，实际 %s", b.Scope)
			}
			if b.Height != 3 {
				t.Fatalf("补齐后 T 高度应为 3（G,S1,P,T），实际 %d", b.Height)
			}
		}
	}
}

// 即使比较发生在父块缺失时（旧比较把 T 视为兼容），只要接纳时已有
// 证据证明 T 与保留方检查点确属不同分叉，就必须拒绝；反之旧比较
// 标记的未知关系，在父块证明为正常后续后应允许重交合并决定接纳。
func TestMergeRejudgesWithCurrentBlocks(t *testing.T) {
	blocks, la, lb, lateParent := missingParentFixture()
	cmp := CompareLines(la, lb, blocks, "C1")

	// 场景一：父块到达后证明 T 是 S1 之后的正常分叉冲突：构造
	// 甲方检查点 S1 之后确认另一条子链，使 P->S1 仍属于 S1 后续
	// （兼容）。这里改为验证“迟到父块证明为分叉冲突”：新增甲方
	// 已确认的检查点 Q（G->S1->Q），而 P 从 S1 另一子位延伸，
	// T 经 P 仍从 S1 延伸，因此不冲突。真正冲突由独立根 R 链给出。
	rival := model.Block{Hash: "R", PrevHash: "G", Work: 10, Txs: nil}
	lb.Blocks = append(lb.Blocks, "R")
	blocksWithRival := append(append([]model.Block{}, blocks...), rival)
	cmp2 := CompareLines(la, lb, blocksWithRival, "C2")
	hasR := false
	for _, c := range cmp2.Conflicts {
		if c.Hash == "R" {
			hasR = true
		}
	}
	if !hasR {
		t.Fatalf("父链完整且从 G 另一子位延伸的 R 与检查点 S1 确属冲突，应记录")
	}
	if _, err := MergeLinesWithBlocks(la, lb, blocksWithRival, cmp2, MergeSpec{
		Name: "x", KeepConfirmation: "a", Accept: []string{"R"},
	}, "LX"); err == nil {
		t.Fatalf("有证据证明跨越 S1 的 R 必须被拒绝接纳")
	}

	// 场景二：旧比较 C1 之后父块 P 到达并证明 T 是 S1 的正常后续，
	// 用旧比较重交合并决定也应成功（按当前区块重判）；随后 P 补交进入
	// 合并线，T 重新判断为 S1 的正常后续。
	withParent := append(append([]model.Block{}, blocks...), lateParent)
	merged, err := MergeLinesWithBlocks(la, lb, withParent, cmp, MergeSpec{
		Name: "父块已到", KeepConfirmation: "a", Accept: []string{"T"},
	}, "LM2")
	if err != nil {
		t.Fatalf("父块证明 T 为 S1 后续后，重交合并决定应成功: %v", err)
	}
	merged.Blocks = append(merged.Blocks, "P")
	merged.Adoptions = append(merged.Adoptions, model.Adoption{
		Hash: "P", ComparisonID: "C1", SourceLine: "LB", AdoptedAt: time.Now().UTC().Format(time.RFC3339),
	})
	snap := BuildLineSnapshot(merged, withParent, 1, "合并", nil)
	if len(snap.Line.PendingGaps) != 0 {
		t.Fatalf("父块 P 补交后不应再有待补充范围，实际 %+v", snap.Line.PendingGaps)
	}
	foundT := false
	for _, b := range snap.Blocks {
		if b.Hash == "T" {
			foundT = true
			if b.Scope != model.ScopeTentative {
				t.Fatalf("T 应为 S1 之后的暂定后续，实际 %s", b.Scope)
			}
		}
	}
	if !foundT {
		t.Fatalf("合并线应包含 T")
	}
}
