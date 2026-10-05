package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chainresearch/internal/model"
	"chainresearch/internal/store"
)

func lineTestBlocks() []model.Block {
	tx := func(id string, ins ...string) model.Transaction {
		t := model.Transaction{TxID: id, Outputs: []model.TxOutput{{Value: 1}}}
		for _, in := range ins {
			t.Inputs = append(t.Inputs, model.TxInput{TxID: in, Vout: 0})
		}
		return t
	}
	return []model.Block{
		{Hash: "G", PrevHash: "", Work: 100, Txs: []model.Transaction{tx("coin-G")}},
		{Hash: "S1", PrevHash: "G", Work: 10, Txs: []model.Transaction{tx("coin-S1", "coin-G")}},
		{Hash: "A2", PrevHash: "S1", Work: 10, Txs: []model.Transaction{tx("txX", "coin-S1")}},
		{Hash: "A3", PrevHash: "A2", Work: 10, Txs: []model.Transaction{tx("txA3", "txX")}},
		{Hash: "A4", PrevHash: "A3", Work: 10, Txs: []model.Transaction{tx("txA4", "txA3")}},
		{Hash: "B2", PrevHash: "S1", Work: 10, Txs: []model.Transaction{tx("txX", "coin-S1")}},
		{Hash: "B3", PrevHash: "B2", Work: 10, Txs: []model.Transaction{tx("txB3", "txX")}},
		{Hash: "B4", PrevHash: "B3", Work: 10, Txs: []model.Transaction{tx("txB4", "txB3")}},
	}
}

func postJSON(t *testing.T, srv *Server, path string, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func getJSON(t *testing.T, srv *Server, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func importToLine(t *testing.T, srv *Server, lineID string, blocks ...model.Block) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"label": "导入" + lineID, "line_id": lineID, "blocks": blocks,
	})
	code, resp := postJSON(t, srv, "/api/import", string(payload))
	if code != http.StatusOK {
		t.Fatalf("导入到 %s 失败: %d %v", lineID, code, resp)
	}
	if lineID != "" && resp["line_state"] == nil {
		t.Fatalf("导入研究线应返回线快照: %v", resp)
	}
}

// 完整流程：两线各自导入与确认 -> 比较 -> 合并 -> 迟到资料 ->
// 撤回未确认资料 -> 重启后复核采用决定来源。
func TestResearchLineCompareMergeFlow(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st)
	blocks := lineTestBlocks()
	byHash := map[string]model.Block{}
	for _, b := range blocks {
		byHash[b.Hash] = b
	}

	// 两位研究者创建研究线，各自从相同资料出发。
	_, respA := postJSON(t, srv, "/api/lines", `{"name":"研究线 A"}`)
	lineA := respA["line"].(map[string]any)["id"].(string)
	_, respB := postJSON(t, srv, "/api/lines", `{"name":"研究线 B"}`)
	lineB := respB["line"].(map[string]any)["id"].(string)

	// 相同资料分别进入两条线；随后各自导入新的区块与交易引用。
	importToLine(t, srv, lineA, byHash["G"], byHash["S1"])
	importToLine(t, srv, lineB, byHash["G"], byHash["S1"])
	importToLine(t, srv, lineA, byHash["A2"], byHash["A3"], byHash["A4"])
	importToLine(t, srv, lineB, byHash["B2"], byHash["B3"], byHash["B4"])

	// 两条研究线建立不同确认检查点。
	code, _ := postJSON(t, srv, "/api/lines/checkpoint", fmt.Sprintf(`{"line":%q,"hash":"S1"}`, lineA))
	if code != http.StatusOK {
		t.Fatalf("A 线确认 S1 失败: %d", code)
	}
	code, _ = postJSON(t, srv, "/api/lines/checkpoint", fmt.Sprintf(`{"line":%q,"hash":"A2"}`, lineA))
	if code != http.StatusOK {
		t.Fatalf("A 线确认检查点失败: %d", code)
	}
	code, _ = postJSON(t, srv, "/api/lines/checkpoint", fmt.Sprintf(`{"line":%q,"hash":"B2"}`, lineB))
	if code != http.StatusOK {
		t.Fatalf("B 线确认 B2 检查点失败: %d", code)
	}

	// 比较两条研究线。
	code, resp := postJSON(t, srv, "/api/lines/compare", fmt.Sprintf(`{"a":%q,"b":%q}`, lineA, lineB))
	if code != http.StatusOK {
		t.Fatalf("比较失败: %d %v", code, resp)
	}
	cmp := resp["comparison"].(map[string]any)
	cmpID := cmp["id"].(string)
	if got := fmt.Sprint(cmp["shared"]); got != "[G S1]" {
		t.Fatalf("共有事实应为 [G S1]，实际 %s", got)
	}
	if len(cmp["compatible_a"].([]any)) != 0 || len(cmp["compatible_b"].([]any)) != 0 {
		t.Fatalf("不同检查点后的独有资料均应进入冲突集，实际 %v/%v", cmp["compatible_a"], cmp["compatible_b"])
	}
	conflicts := cmp["conflicts"].([]any)
	if len(conflicts) != 6 {
		t.Fatalf("A2-A4 与 B2-B4 互相跨越检查点，应有 6 个冲突，实际 %v", conflicts)
	}
	txNotes := cmp["tx_notes"].([]any)
	if len(txNotes) == 0 {
		t.Fatalf("txX 在两条线不同分叉的出现应分别说明")
	}

	// 合并：保留 A 线确认；A 线确认范围完整进入新线，只接纳 A4（缺少 A3 -> 待补充）。
	code, resp = postJSON(t, srv, "/api/lines/merge", fmt.Sprintf(
		`{"comparison_id":%q,"name":"保留甲方","keep":"a","accept":["A4"]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("合并失败: %d %v", code, resp)
	}
	mergedA := resp["line"].(map[string]any)["id"].(string)
	state := resp["state"].(map[string]any)
	checkpoint := state["checkpoint"].(map[string]any)
	if checkpoint["hash"] != "A2" {
		t.Fatalf("保留甲方确认时新线应继承 A2 检查点，实际 %v", checkpoint)
	}
	lineView := state["line"].(map[string]any)
	gaps := lineView["pending_gaps"].([]any)
	if len(gaps) != 1 {
		t.Fatalf("缺少中间资料 A3 应保留待补充范围，实际 %v", gaps)
	}
	gap := gaps[0].(map[string]any)
	if gap["hash"] != "A4" || fmt.Sprint(gap["missing_parents"]) != "[A3]" || gap["source_line"] != lineA {
		t.Fatalf("待接纳资料应保留 A4 的待补充状态和 LA 出处，实际 %v", gap)
	}
	// 只有乙方越过新线 A2 的冲突暂缓；A2 自身相对 B2 的冲突不能删除甲方确认。
	deferred := lineView["deferred"].([]any)
	if len(deferred) != 3 {
		t.Fatalf("B2/B3/B4 应记入暂缓，实际 %v", deferred)
	}
	// 采用决定记录来源比较。
	adoptions := lineView["adoptions"].(map[string]any)
	if adoptions["A2"].(map[string]any)["source_line"] != lineA ||
		adoptions["A4"].(map[string]any)["comparison_id"] != cmpID {
		t.Fatalf("A2/A4 的采用依据应可追溯到来源线和比较 %s，实际 %v", cmpID, adoptions)
	}

	// 同样的合并决定也可选择保留乙方确认，生成另一条独立新线。
	code, resp = postJSON(t, srv, "/api/lines/merge", fmt.Sprintf(
		`{"comparison_id":%q,"name":"保留乙方","keep":"b","accept":["B4"]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("保留乙方合并失败: %d %v", code, resp)
	}
	mergedB := resp["line"].(map[string]any)["id"].(string)
	stateB := resp["state"].(map[string]any)
	if stateB["checkpoint"].(map[string]any)["hash"] != "B2" {
		t.Fatalf("保留乙方确认时新线应继承 B2 检查点，实际 %v", stateB["checkpoint"])
	}
	lineViewB := stateB["line"].(map[string]any)
	if len(lineViewB["deferred"].([]any)) != 3 || len(lineViewB["pending_gaps"].([]any)) != 1 {
		t.Fatalf("保留乙方线应暂缓 A2-A4 且 B4 缺少 B3，实际 %v", lineViewB)
	}

	// 迟到父块到达：把 A3 导入甲方合并线，待补充范围消失，并补记出处。
	importToLine(t, srv, mergedA, byHash["A3"])
	code, resp = getJSON(t, srv, "/api/lines/state?line="+mergedA)
	if code != http.StatusOK {
		t.Fatalf("读取合并线状态失败: %d", code)
	}
	lineView = resp["snapshot"].(map[string]any)["line"].(map[string]any)
	adoptions = lineView["adoptions"].(map[string]any)
	if gaps := lineView["pending_gaps"].([]any); len(gaps) != 0 {
		t.Fatalf("迟到资料到达后待补充范围应消失，实际 %v", gaps)
	}
	if adoptions["A3"].(map[string]any)["source_line"] != lineA ||
		adoptions["A3"].(map[string]any)["comparison_id"] != cmpID {
		t.Fatalf("补交 A3 后应保留其来源线和合并依据，实际 %v", adoptions)
	}

	// 撤回未确认资料 A4：引用 txA4->txA3 随资料撤回。
	code, resp = postJSON(t, srv, "/api/lines/withdraw", fmt.Sprintf(`{"line":%q,"hash":"A4"}`, mergedA))
	if code != http.StatusOK {
		t.Fatalf("撤回未确认资料失败: %d %v", code, resp)
	}
	refs := resp["state"].(map[string]any)["tx_refs"].([]any)
	for _, r := range refs {
		ref := r.(map[string]any)
		if ref["from_txid"] == "txA4" {
			t.Fatalf("撤回 A4 后不应再出现 txA4 发出的引用: %v", ref)
		}
	}
	// 已确认范围不能撤回（合并线保留了 A 线的 S1 检查点；
	// A2 是继承的确认范围，不能撤回。
	code, _ = postJSON(t, srv, "/api/lines/withdraw", fmt.Sprintf(`{"line":%q,"hash":"S1"}`, mergedA))
	if code != http.StatusConflict {
		t.Fatalf("撤回已确认范围应被拒绝，实际 %d", code)
	}

	// 重复合并：同一比较再次合并生成另一条新线，互不影响。
	code, resp = postJSON(t, srv, "/api/lines/merge", fmt.Sprintf(
		`{"comparison_id":%q,"name":"再次合并","keep":"a","accept":[]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("重复合并失败: %d %v", code, resp)
	}
	secondID := resp["line"].(map[string]any)["id"].(string)
	if secondID == mergedA || secondID == mergedB {
		t.Fatalf("重复合并应生成新的研究线")
	}

	// 保存重开后：每个采用决定来自哪次比较仍可复核。
	st2, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv2 := New(st2)
	code, resp = getJSON(t, srv2, "/api/lines/state?line="+mergedA)
	if code != http.StatusOK {
		t.Fatalf("重开后读取合并线失败: %d", code)
	}
	snap := resp["snapshot"].(map[string]any)
	lineView = snap["line"].(map[string]any)
	adoptions = lineView["adoptions"].(map[string]any)
	if adoptions["G"].(map[string]any)["comparison_id"] != cmpID ||
		adoptions["G"].(map[string]any)["source_line"] != "both" {
		t.Fatalf("重开后采用决定来源应可复核，实际 %v", adoptions)
	}
	if lineView["base_comparison"] != cmpID {
		t.Fatalf("合并线应记录其来源比较 %s，实际 %v", cmpID, lineView["base_comparison"])
	}
	// 原有两条线及其确认依据保持可回看。
	code, resp = getJSON(t, srv2, "/api/lines/state?line="+lineA)
	if code != http.StatusOK {
		t.Fatalf("原研究线 A 应可回看: %d", code)
	}
	cp := resp["snapshot"].(map[string]any)["checkpoint"]
	if cp == nil || cp.(map[string]any)["hash"] != "A2" {
		t.Fatalf("原研究线 A 的确认依据应保留，实际 %v", cp)
	}
	// 比较历史持久化。
	code, resp = getJSON(t, srv2, "/api/lines/comparisons")
	if code != http.StatusOK || len(resp["comparisons"].([]any)) == 0 {
		t.Fatalf("重开后比较历史应可查询: %d %v", code, resp)
	}
	// 研究线版本序列独立：合并线有多个版本且可回看任一版本。
	code, resp = getJSON(t, srv2, "/api/lines/versions?line="+mergedA)
	if code != http.StatusOK || len(resp["versions"].([]any)) < 3 {
		t.Fatalf("合并线应有独立版本序列（合并/迟到资料/撤回），实际 %v", resp)
	}
}

func TestLineCheckpointRejectsForeignBlock(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st)
	_, resp := postJSON(t, srv, "/api/lines", `{"name":"L"}`)
	lineID := resp["line"].(map[string]any)["id"].(string)
	blocks := lineTestBlocks()
	importToLine(t, srv, lineID, blocks[0])
	code, _ := postJSON(t, srv, "/api/lines/checkpoint",
		fmt.Sprintf(`{"line":%q,"hash":"S1"}`, lineID))
	if code != http.StatusBadRequest {
		t.Fatalf("不属于研究线的区块不能在该线确认，实际 %d", code)
	}
}

// 缺陷场景：乙方提交的 gap-tip T 缺少父块 P（P 全局也未导入），
// 且甲方已确认 S1。T 必须作为有出处的待补充项被接纳保留，而不是
// 当作不能接纳的资料直接拒绝；父块 P 迟到（指向已确认 S1）后重新
// 判断为已选确认的后续。同时验证重复合并、撤回、重启与原线保留。
func TestResearchLineGapTipKeptThenLateParentFlow(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(st)
	mkBlock := func(hash, prev string, txid string, ins ...string) model.Block {
		tr := model.Transaction{TxID: txid, Outputs: []model.TxOutput{{Value: 1}}}
		for _, in := range ins {
			tr.Inputs = append(tr.Inputs, model.TxInput{TxID: in, Vout: 0})
		}
		return model.Block{Hash: hash, PrevHash: prev, Work: 10, Txs: []model.Transaction{tr}}
	}
	bG := mkBlock("G", "", "coin-G")
	bS1 := mkBlock("S1", "G", "coin-S1", "coin-G")
	bT := mkBlock("T", "P", "txT", "coin-S1")  // 父块 P 此刻全局缺失
	bP := mkBlock("P", "S1", "txP", "coin-S1") // 迟到父块，指向已确认 S1

	_, respA := postJSON(t, srv, "/api/lines", `{"name":"甲方线"}`)
	lineA := respA["line"].(map[string]any)["id"].(string)
	_, respB := postJSON(t, srv, "/api/lines", `{"name":"乙方线"}`)
	lineB := respB["line"].(map[string]any)["id"].(string)

	importToLine(t, srv, lineA, bG, bS1)
	importToLine(t, srv, lineB, bG, bS1, bT)
	code, _ := postJSON(t, srv, "/api/lines/checkpoint",
		fmt.Sprintf(`{"line":%q,"hash":"S1"}`, lineA))
	if code != http.StatusOK {
		t.Fatalf("甲方确认 S1 失败: %d", code)
	}
	code, resp := postJSON(t, srv, "/api/lines/compare",
		fmt.Sprintf(`{"a":%q,"b":%q}`, lineA, lineB))
	if code != http.StatusOK {
		t.Fatalf("比较失败: %d %v", code, resp)
	}
	cmpObj := resp["comparison"].(map[string]any)
	cmpID := cmpObj["id"].(string)
	if len(cmpObj["conflicts"].([]any)) != 0 {
		t.Fatalf("父块缺失时 T 不能被记为冲突，实际 %v", cmpObj["conflicts"])
	}
	if got := fmt.Sprint(cmpObj["compatible_b"]); got != "[T]" {
		t.Fatalf("T 应作为乙方可接纳的待补充资料，实际 %s", got)
	}

	// 保留甲方确认并接纳缺父块的 T：应成功，T 在快照中保留待补充状态与乙方出处。
	code, resp = postJSON(t, srv, "/api/lines/merge",
		fmt.Sprintf(`{"comparison_id":%q,"name":"合并-甲","keep":"a","accept":["T"]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("缺父块的 gap-tip 应被接纳保留，不应直接拒绝: %d %v", code, resp)
	}
	merged := resp["line"].(map[string]any)["id"].(string)
	state := resp["state"].(map[string]any)
	view := state["line"].(map[string]any)
	gaps := view["pending_gaps"].([]any)
	if len(gaps) != 1 {
		t.Fatalf("应有 1 条待补充范围，实际 %v", gaps)
	}
	gap := gaps[0].(map[string]any)
	if gap["hash"] != "T" || fmt.Sprint(gap["missing_parents"]) != "[P]" ||
		gap["source_line"] != lineB {
		t.Fatalf("T 应保留缺父块 P 的待补充状态及乙方出处，实际 %v", gap)
	}
	// 待接纳状态下链视图不拼接：T 高度 0、范围 pending；无冲突链汇总。
	if conflicts := state["conflicts"].([]any); len(conflicts) != 0 {
		t.Fatalf("缺父块资料不能汇总为冲突链，实际 %v", conflicts)
	}
	var tBlock map[string]any
	for _, blk := range state["blocks"].([]any) {
		b := blk.(map[string]any)
		if b["hash"] == "T" {
			tBlock = b
		}
	}
	if tBlock == nil || tBlock["height"].(float64) != 0 || tBlock["scope"] != "pending" {
		t.Fatalf("待补充的 T 应为高度 0、范围 pending，实际 %+v", tBlock)
	}
	// 交易引用 txT->coin-S1：目标已在确认范围，但引用方 T 待补充，
	// 引用不应提前认作当前有效。
	for _, rAny := range state["tx_refs"].([]any) {
		r := rAny.(map[string]any)
		if r["from_txid"] == "txT" && r["active"] == true {
			t.Fatalf("待补充资料 T 发出的引用不能提前认作有效: %+v", r)
		}
	}

	// 重复提交同一合并决定：生成另一条独立新线，互不影响。
	code, resp = postJSON(t, srv, "/api/lines/merge",
		fmt.Sprintf(`{"comparison_id":%q,"name":"合并-甲-再次","keep":"a","accept":["T"]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("重复合并应成功: %d %v", code, resp)
	}
	mergedAgain := resp["line"].(map[string]any)["id"].(string)
	if mergedAgain == merged {
		t.Fatalf("重复合并应生成新研究线")
	}
	// 保留乙方确认同样可生成一条独立新线。
	code, resp = postJSON(t, srv, "/api/lines/merge",
		fmt.Sprintf(`{"comparison_id":%q,"name":"合并-乙","keep":"b","accept":["T"]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("保留乙方合并应成功: %d %v", code, resp)
	}
	_ = resp["line"].(map[string]any)["id"].(string) // 保留乙方的合并线同样保留
	if resp["state"].(map[string]any)["checkpoint"] != nil {
		// 乙方未确认检查点，新线不应携带检查点。
		t.Fatalf("乙方无确认，合并线不应继承检查点")
	}

	// 撤回未确认资料 T（尚未补父块，T 为未确认待补充资料）：允许。
	code, resp = postJSON(t, srv, "/api/lines/withdraw",
		fmt.Sprintf(`{"line":%q,"hash":"T"}`, merged))
	if code != http.StatusOK {
		t.Fatalf("待补充的未确认资料 T 应允许撤回: %d %v", code, resp)
	}
	for _, blk := range resp["state"].(map[string]any)["blocks"].([]any) {
		if blk.(map[string]any)["hash"] == "T" {
			t.Fatalf("撤回后 T 不应再出现在链视图")
		}
	}
	// 已确认范围不能撤回。
	code, _ = postJSON(t, srv, "/api/lines/withdraw",
		fmt.Sprintf(`{"line":%q,"hash":"S1"}`, merged))
	if code != http.StatusConflict {
		t.Fatalf("撤回已确认范围应 409，实际 %d", code)
	}

	// 在“再次合并”线上补交迟到父块 P：缺口消失，T 重判为 S1 后续。
	importToLine(t, srv, mergedAgain, bP)
	code, resp = getJSON(t, srv, "/api/lines/state?line="+mergedAgain)
	if code != http.StatusOK {
		t.Fatalf("读取再次合并线失败: %d", code)
	}
	snap2 := resp["snapshot"].(map[string]any)
	view2 := snap2["line"].(map[string]any)
	if gs := view2["pending_gaps"].([]any); len(gs) != 0 {
		t.Fatalf("父块 P 到达后待补充范围应消失，实际 %v", gs)
	}
	var t2 map[string]any
	for _, blk := range snap2["blocks"].([]any) {
		b := blk.(map[string]any)
		if b["hash"] == "T" {
			t2 = b
		}
	}
	if t2 == nil || t2["height"].(float64) != 3 || t2["scope"] != "tentative" {
		t.Fatalf("补齐后 T 应为高度 3、暂定后续，实际 %+v", t2)
	}
	// 采用审计：P 补交后仍记录来源比较与乙方出处。
	adoptions := view2["adoptions"].(map[string]any)
	if ap, ok := adoptions["P"].(map[string]any); !ok ||
		ap["comparison_id"] != cmpID || ap["source_line"] != lineB {
		t.Fatalf("补交 P 应保留来源比较与乙方出处，实际 %+v", adoptions)
	}

	// 重启后复核：待补充/采用决定、确认范围与版本序列一致，原线保留。
	st2, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv2 := New(st2)
	code, resp = getJSON(t, srv2, "/api/lines/state?line="+mergedAgain)
	if code != http.StatusOK {
		t.Fatalf("重启后读取合并线失败: %d", code)
	}
	view3 := resp["snapshot"].(map[string]any)["line"].(map[string]any)
	if len(view3["pending_gaps"].([]any)) != 0 {
		t.Fatalf("重启后 T 仍应是已补齐的正常后续")
	}
	// 原甲方研究线及其确认始终保留可回看。
	code, resp = getJSON(t, srv2, "/api/lines/state?line="+lineA)
	if code != http.StatusOK {
		t.Fatalf("原甲方线应保留: %d", code)
	}
	cp := resp["snapshot"].(map[string]any)["checkpoint"]
	if cp == nil || cp.(map[string]any)["hash"] != "S1" {
		t.Fatalf("原甲方线确认 S1 应保留，实际 %v", cp)
	}
	// 原乙方线也保留。
	code, _ = getJSON(t, srv2, "/api/lines/state?line="+lineB)
	if code != http.StatusOK {
		t.Fatalf("原乙方线应保留: %d", code)
	}
	// 合并线数量：merged、mergedAgain、mergedB 三条，且原两线仍在。
	code, resp = getJSON(t, srv2, "/api/lines")
	if code != http.StatusOK {
		t.Fatalf("列出研究线失败: %d", code)
	}
	if n := len(resp["lines"].([]any)); n != 5 {
		t.Fatalf("原两线 + 三条合并线应共 5 条，实际 %d", n)
	}
}
