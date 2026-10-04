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
		{Hash: "B2", PrevHash: "S1", Work: 10, Txs: []model.Transaction{tx("txX", "coin-S1")}},
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
	importToLine(t, srv, lineA, byHash["A2"], byHash["A3"])
	importToLine(t, srv, lineB, byHash["B2"])

	// 从相同资料建立不同的确认检查点：两线都确认过共有的 S1，
	// 随后 A 线又确认了自己后续导入的 A2。
	code, _ := postJSON(t, srv, "/api/lines/checkpoint", fmt.Sprintf(`{"line":%q,"hash":"S1"}`, lineA))
	if code != http.StatusOK {
		t.Fatalf("A 线确认 S1 失败: %d", code)
	}
	code, _ = postJSON(t, srv, "/api/lines/checkpoint", fmt.Sprintf(`{"line":%q,"hash":"A2"}`, lineA))
	if code != http.StatusOK {
		t.Fatalf("A 线确认检查点失败: %d", code)
	}
	code, _ = postJSON(t, srv, "/api/lines/checkpoint", fmt.Sprintf(`{"line":%q,"hash":"S1"}`, lineB))
	if code != http.StatusOK {
		t.Fatalf("B 线确认检查点失败: %d", code)
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
	if got := fmt.Sprint(cmp["compatible_a"]); got != "[A2 A3]" {
		t.Fatalf("A 线兼容后续资料应为 [A2 A3]（均为 B 线检查点 S1 的延伸），实际 %s", got)
	}
	conflicts := cmp["conflicts"].([]any)
	if len(conflicts) != 1 {
		t.Fatalf("B2 跨越 A 线检查点 A2 应为冲突，实际 %v", conflicts)
	}
	txNotes := cmp["tx_notes"].([]any)
	if len(txNotes) == 0 {
		t.Fatalf("txX 在两条线不同分叉的出现应分别说明")
	}

	// 合并：保留 A 线确认，只接纳 A3。
	// 明确选择的确认范围（A2 及其祖先）完整进入新线，
	// 甲方确认检查点及其依据不被遗漏。
	code, resp = postJSON(t, srv, "/api/lines/merge", fmt.Sprintf(
		`{"comparison_id":%q,"name":"合并线","keep":"a","accept":["A3"]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("合并失败: %d %v", code, resp)
	}
	mergedID := resp["line"].(map[string]any)["id"].(string)
	state := resp["state"].(map[string]any)
	lineView := state["line"].(map[string]any)
	if cp := state["checkpoint"]; cp == nil || cp.(map[string]any)["hash"] != "A2" {
		t.Fatalf("保留甲方确认后合并线检查点应为 A2，实际 %v", cp)
	}
	gaps := lineView["pending_gaps"].([]any)
	if len(gaps) != 0 {
		t.Fatalf("确认范围完整进入新线后不应有待补充范围，实际 %v", gaps)
	}
	// 冲突部分暂缓而非无效。
	deferred := lineView["deferred"].([]any)
	if len(deferred) != 1 {
		t.Fatalf("B2 应记入暂缓，实际 %v", deferred)
	}
	// 采用决定记录来源比较：确认范围 A2 来自 A 线，A3 为勾选接纳。
	adoptions := lineView["adoptions"].(map[string]any)
	if adoptions["A2"].(map[string]any)["comparison_id"] != cmpID ||
		adoptions["A2"].(map[string]any)["source_line"] != lineA {
		t.Fatalf("确认范围 A2 的采用依据应来自比较 %s 与研究线 A，实际 %v", cmpID, adoptions)
	}
	if adoptions["A3"].(map[string]any)["comparison_id"] != cmpID {
		t.Fatalf("A3 的采用决定应来自比较 %s，实际 %v", cmpID, adoptions)
	}

	// 重复提交同一合并决定：生成新的研究线，互不影响。
	code, resp = postJSON(t, srv, "/api/lines/merge", fmt.Sprintf(
		`{"comparison_id":%q,"name":"再次合并","keep":"a","accept":["A3"]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("重复合并失败: %d %v", code, resp)
	}
	if dupID := resp["line"].(map[string]any)["id"].(string); dupID == mergedID {
		t.Fatalf("重复合并应生成新的研究线")
	}

	// 保留乙方确认、只接纳 A3：缺少中间区块 A2 的待接纳资料
	// 保留待补充状态与出处，不被当成无效资料拒绝。
	code, resp = postJSON(t, srv, "/api/lines/merge", fmt.Sprintf(
		`{"comparison_id":%q,"name":"部分接纳","keep":"b","accept":["A3"]}`, cmpID))
	if code != http.StatusOK {
		t.Fatalf("缺少父块的待接纳资料不应被拒绝: %d %v", code, resp)
	}
	partialID := resp["line"].(map[string]any)["id"].(string)
	lineView = resp["state"].(map[string]any)["line"].(map[string]any)
	gaps = lineView["pending_gaps"].([]any)
	if len(gaps) != 1 {
		t.Fatalf("缺少中间资料 A2 应保留待补充范围，实际 %v", gaps)
	}
	adoptions = lineView["adoptions"].(map[string]any)
	if adoptions["A3"].(map[string]any)["comparison_id"] != cmpID {
		t.Fatalf("待补充资料 A3 应保留采用出处，实际 %v", adoptions)
	}

	// 补交迟到父块：把 A2 导入该线，待补充范围消失。
	importToLine(t, srv, partialID, byHash["A2"])
	code, resp = getJSON(t, srv, "/api/lines/state?line="+partialID)
	if code != http.StatusOK {
		t.Fatalf("读取部分接纳线状态失败: %d", code)
	}
	lineView = resp["snapshot"].(map[string]any)["line"].(map[string]any)
	if gaps := lineView["pending_gaps"].([]any); len(gaps) != 0 {
		t.Fatalf("迟到资料到达后待补充范围应消失，实际 %v", gaps)
	}

	// 撤回未确认资料 A3：引用 txA3->txX 失去独立来源。
	code, resp = postJSON(t, srv, "/api/lines/withdraw", fmt.Sprintf(`{"line":%q,"hash":"A3"}`, partialID))
	if code != http.StatusOK {
		t.Fatalf("撤回未确认资料失败: %d %v", code, resp)
	}
	refs := resp["state"].(map[string]any)["tx_refs"].([]any)
	for _, r := range refs {
		ref := r.(map[string]any)
		if ref["from_txid"] == "txA3" {
			t.Fatalf("撤回 A3 后不应再出现 txA3 发出的引用: %v", ref)
		}
	}
	// 已确认范围不能撤回：合并线完整保留了 A 线的确认（S1 与 A2）。
	code, _ = postJSON(t, srv, "/api/lines/withdraw", fmt.Sprintf(`{"line":%q,"hash":"S1"}`, mergedID))
	if code != http.StatusConflict {
		t.Fatalf("撤回已确认范围应被拒绝，实际 %d", code)
	}
	code, _ = postJSON(t, srv, "/api/lines/withdraw", fmt.Sprintf(`{"line":%q,"hash":"A2"}`, mergedID))
	if code != http.StatusConflict {
		t.Fatalf("撤回已确认检查点 A2 应被拒绝，实际 %d", code)
	}

	// 保存重开后：每个采用决定来自哪次比较仍可复核。
	st2, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	srv2 := New(st2)
	code, resp = getJSON(t, srv2, "/api/lines/state?line="+mergedID)
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
	// 重开后合并线的确认检查点及其依据仍然完整。
	if cp := snap["checkpoint"]; cp == nil || cp.(map[string]any)["hash"] != "A2" {
		t.Fatalf("重开后合并线检查点应为 A2，实际 %v", cp)
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
	// 研究线版本序列独立：部分接纳线有多个版本且可回看任一版本。
	code, resp = getJSON(t, srv2, "/api/lines/versions?line="+partialID)
	if code != http.StatusOK || len(resp["versions"].([]any)) < 3 {
		t.Fatalf("部分接纳线应有独立版本序列（合并/迟到父块/撤回），实际 %v", resp)
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
