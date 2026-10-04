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
