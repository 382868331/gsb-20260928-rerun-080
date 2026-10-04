package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"chainresearch/internal/model"
	"chainresearch/internal/sample"
	"chainresearch/internal/store"
)

type studyTestEnv struct {
	mux *http.ServeMux
	dir string
}

func newStudyTestEnv(t *testing.T) studyTestEnv {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return studyTestEnv{mux: New(s).Mux, dir: dir}
}

func (e studyTestEnv) do(method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	e.mux.ServeHTTP(rw, req)
	return rw
}

func (e studyTestEnv) importTo(t *testing.T, study string, blocks []model.Block) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"blocks": blocks, "label": "导入 " + study, "study": study})
	if rw := e.do(http.MethodPost, "/api/import", string(payload)); rw.Code != 200 {
		t.Fatalf("导入 %s 失败: %d %s", study, rw.Code, rw.Body.String())
	}
}

func sampleByHash(t *testing.T, hashes ...string) []model.Block {
	t.Helper()
	want := map[string]bool{}
	for _, h := range hashes {
		want[h] = true
	}
	var out []model.Block
	for _, b := range sample.Blocks() {
		if want[b.Hash] {
			out = append(out, b)
		}
	}
	if len(out) != len(hashes) {
		t.Fatalf("样例缺少区块: %v", hashes)
	}
	return out
}

// 两位研究者从相同资料出发，各自确认不同检查点并导入后续资料；
// 比较应区分共有事实、兼容后续与跨越确认边界的冲突。
func TestCompareTwoResearchLines(t *testing.T) {
	env := newStudyTestEnv(t)
	env.importTo(t, "main", sampleByHash(t, "G", "A1", "A2", "A3"))
	// 研究者甲确认 A1 为检查点。
	cpPayload, _ := json.Marshal(map[string]any{"hash": "A1", "study": "main"})
	if rw := env.do(http.MethodPost, "/api/checkpoint", string(cpPayload)); rw.Code != 200 {
		t.Fatalf("确认检查点失败: %s", rw.Body.String())
	}
	// 研究者乙从同一份 G 出发，建立独立研究线并导入 B 分叉。
	if rw := env.do(http.MethodPost, "/api/studies", `{"id":"b","name":"乙的研究线"}`); rw.Code != 200 {
		t.Fatalf("创建研究线失败: %s", rw.Body.String())
	}
	env.importTo(t, "b", sampleByHash(t, "G", "B1", "B2"))

	rw := env.do(http.MethodGet, "/api/compare?a=main&b=b", "")
	if rw.Code != 200 {
		t.Fatalf("比较失败: %d %s", rw.Code, rw.Body.String())
	}
	var cmp model.CompareResult
	if err := json.Unmarshal(rw.Body.Bytes(), &cmp); err != nil {
		t.Fatal(err)
	}
	// 共有事实按区块身份判断：只有 G。
	if len(cmp.Shared) != 1 || cmp.Shared[0].Hash != "G" {
		t.Fatalf("共有区块应为 [G]: %+v", cmp.Shared)
	}
	// 甲线独有的 A1/A2/A3 相对乙线（无检查点）是兼容后续（接到 G）。
	for _, b := range cmp.OnlyA {
		if b.Relation != model.RelCompatible {
			t.Fatalf("A 线区块 %s 应为兼容后续，实际 %s", b.Hash, b.Relation)
		}
	}
	// 乙线的 B1/B2 不经过甲线检查点 A1：跨越确认边界的冲突，
	// 不能因为乙线更新就自动撤销甲的确认。
	if len(cmp.OnlyB) != 2 {
		t.Fatalf("B 线应有 2 个独有区块: %+v", cmp.OnlyB)
	}
	for _, b := range cmp.OnlyB {
		if b.Relation != model.RelConflict {
			t.Fatalf("B 线区块 %s 应为冲突，实际 %s", b.Hash, b.Relation)
		}
	}
	// 同一交易 txA 在两条线里的引用分别列出。
	if len(cmp.RefsOnlyA) == 0 || len(cmp.RefsOnlyB) == 0 {
		t.Fatalf("两条线应各有独有引用: A=%d B=%d", len(cmp.RefsOnlyA), len(cmp.RefsOnlyB))
	}
}

// 合并只接纳未确认的一段资料：核对父区块依据，缺少中间资料时
// 保留待补充范围；保留哪套确认由用户选择；冲突部分暂缓而非无效。
func TestMergeSelectiveAcceptAndPendingGaps(t *testing.T) {
	env := newStudyTestEnv(t)
	env.importTo(t, "main", sampleByHash(t, "G", "A1", "A2", "A3"))
	cpPayload, _ := json.Marshal(map[string]any{"hash": "A1", "study": "main"})
	env.do(http.MethodPost, "/api/checkpoint", string(cpPayload))
	env.do(http.MethodPost, "/api/studies", `{"id":"b","name":"乙的研究线"}`)
	env.importTo(t, "b", sampleByHash(t, "G", "B1", "B2", "B3", "B4"))

	// 只接纳 B2/B3（缺少中间资料 B1），保留甲的确认，B4 暂缓。
	mergePayload, _ := json.Marshal(map[string]any{
		"a": "main", "b": "b", "new_id": "merged", "name": "合并研究线",
		"accept": []string{"B2", "B3"}, "deferred": []string{"B4"},
		"keep_checkpoint": "a",
	})
	rw := env.do(http.MethodPost, "/api/merge", string(mergePayload))
	if rw.Code != 200 {
		t.Fatalf("合并失败: %d %s", rw.Code, rw.Body.String())
	}
	var resp struct {
		Study model.Study           `json:"study"`
		State model.VersionSnapshot `json:"state"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	// 新线 = 共有(G) ∪ 甲线确认段(A1) ∪ 接纳(B2,B3)。
	got := map[string]bool{}
	for _, b := range resp.State.Blocks {
		got[b.Hash] = true
	}
	for _, want := range []string{"G", "A1", "B2", "B3"} {
		if !got[want] {
			t.Fatalf("合并后缺少区块 %s: %v", want, got)
		}
	}
	for _, unwanted := range []string{"A2", "A3", "B1", "B4"} {
		if got[unwanted] {
			t.Fatalf("合并后不应包含 %s", unwanted)
		}
	}
	// 缺少中间资料 B1：保留为待补充范围，不拼接出从未存在的链。
	if len(resp.State.PendingGaps) != 1 || resp.State.PendingGaps[0] != "B1" {
		t.Fatalf("应记录待补充缺口 B1: %+v", resp.State.PendingGaps)
	}
	// 保留甲的确认：检查点 A1 生效；B2/B3 跨越确认边界记为冲突，
	// 甲的确认不被撤销。
	if resp.State.Checkpoint == nil || resp.State.Checkpoint.Hash != "A1" {
		t.Fatalf("合并后应保留甲的检查点 A1: %+v", resp.State.Checkpoint)
	}
	if len(resp.State.Conflicts) == 0 {
		t.Fatal("跨越确认边界的 B 段应记为冲突")
	}
	// 采用决定可复核：接纳、暂缓与缺口都记录在案。
	if len(resp.Study.Decisions) != 1 {
		t.Fatalf("应记录 1 条采用决定: %+v", resp.Study.Decisions)
	}
	d := resp.Study.Decisions[0]
	if d.Kind != "merge" || len(d.Accepted) != 2 || len(d.Deferred) != 1 || d.KeptCheckpoint != "A1" {
		t.Fatalf("采用决定内容异常: %+v", d)
	}

	// 迟到补充共同祖先材料 B1 后，缺口消失。
	env.importTo(t, "merged", sampleByHash(t, "B1"))
	rw = env.do(http.MethodGet, "/api/state?study=merged", "")
	var sr stateResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	if len(sr.Snapshot.PendingGaps) != 0 {
		t.Fatalf("补充 B1 后缺口应消失: %+v", sr.Snapshot.PendingGaps)
	}

	// 原有两条线及其确认依据保持可回看。
	rw = env.do(http.MethodGet, "/api/state?study=main", "")
	var mainState stateResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &mainState); err != nil {
		t.Fatal(err)
	}
	if mainState.Snapshot.Checkpoint == nil || mainState.Snapshot.Checkpoint.Hash != "A1" {
		t.Fatal("原研究线的检查点应保持不变")
	}
	if len(mainState.Snapshot.Blocks) != 4 {
		t.Fatalf("原研究线区块应保持 4 个: %d", len(mainState.Snapshot.Blocks))
	}

	// 保存重开后：新线、采用决定与原线确认依据都可复核。
	s2, err := store.Open(env.dir)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok := s2.StudyMeta("merged")
	if !ok || len(meta.Decisions) != 1 || meta.Decisions[0].KeptCheckpoint != "A1" {
		t.Fatalf("重开后采用决定应可复核: %+v", meta.Decisions)
	}
	cps := s2.CheckpointsOf("main")
	if len(cps) != 1 || cps[0].Hash != "A1" {
		t.Fatalf("重开后原线确认依据应保留: %+v", cps)
	}
	if _, ok := s2.LatestSnapshotOf("merged"); !ok {
		t.Fatal("重开后合并研究线的版本应保留")
	}
}

// 撤回未确认资料后，仍由其他区块独立提供的引用保持有效；
// 确认范围内的区块不允许撤回。
func TestRetractKeepsIndependentRefs(t *testing.T) {
	env := newStudyTestEnv(t)
	// txA 同时出现在 A2 与 B2（同一交易在不同分叉）。
	env.importTo(t, "main", sampleByHash(t, "G", "A1", "A2", "B1", "B2"))
	cpPayload, _ := json.Marshal(map[string]any{"hash": "A1", "study": "main"})
	env.do(http.MethodPost, "/api/checkpoint", string(cpPayload))

	// 确认范围内的 A1 不允许撤回。
	retractCp, _ := json.Marshal(map[string]any{"study": "main", "hash": "A1"})
	if rw := env.do(http.MethodPost, "/api/retract", string(retractCp)); rw.Code == 200 {
		t.Fatal("确认范围内的区块不应允许撤回")
	}

	// 撤回未确认的 B2：txA 在 A2 的引用仍有独立来源。
	retractB2, _ := json.Marshal(map[string]any{"study": "main", "hash": "B2"})
	rw := env.do(http.MethodPost, "/api/retract", string(retractB2))
	if rw.Code != 200 {
		t.Fatalf("撤回失败: %d %s", rw.Code, rw.Body.String())
	}
	var resp struct {
		State model.VersionSnapshot `json:"state"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var txAPos, txARefs int
	for _, p := range resp.State.TxIndex {
		if p.TxID == "txA" {
			txAPos++
			if p.BlockHash != "A2" {
				t.Fatalf("撤回 B2 后 txA 应只剩 A2 位置: %+v", p)
			}
		}
	}
	for _, ref := range resp.State.TxRefs {
		if ref.FromTxID == "txA" {
			txARefs++
			if ref.FromBlockHash != "A2" {
				t.Fatalf("txA 引用应来自 A2 的独立来源: %+v", ref)
			}
		}
	}
	if txAPos != 1 || txARefs != 1 {
		t.Fatalf("撤回后 txA 应保留 1 个位置与 1 条引用: pos=%d refs=%d", txAPos, txARefs)
	}

	// 撤回记录入采用决定，重开后可复核。
	s2, err := store.Open(env.dir)
	if err != nil {
		t.Fatal(err)
	}
	meta, _ := s2.StudyMeta("main")
	found := false
	for _, d := range meta.Decisions {
		if d.Kind == "retract" && d.Retracted == "B2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("重开后撤回应可复核: %+v", meta.Decisions)
	}
}

// 重复合并生成新的研究线，原合并线保持可回看；版本按研究线各自独立。
func TestRepeatedMergeKeepsHistory(t *testing.T) {
	env := newStudyTestEnv(t)
	env.importTo(t, "main", sampleByHash(t, "G", "A1"))
	env.do(http.MethodPost, "/api/studies", `{"id":"b"}`)
	env.importTo(t, "b", sampleByHash(t, "G", "B1"))

	for i, id := range []string{"m1", "m2"} {
		payload, _ := json.Marshal(map[string]any{
			"a": "main", "b": "b", "new_id": id,
			"accept": []string{"B1"}, "keep_checkpoint": "",
		})
		rw := env.do(http.MethodPost, "/api/merge", string(payload))
		if rw.Code != 200 {
			t.Fatalf("第 %d 次合并失败: %s", i+1, rw.Body.String())
		}
	}
	rw := env.do(http.MethodGet, "/api/studies", "")
	var resp struct {
		Studies []struct {
			model.Study
			TipHash string `json:"tip_hash"`
		} `json:"studies"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, st := range resp.Studies {
		ids[st.ID] = true
	}
	for _, want := range []string{"main", "b", "m1", "m2"} {
		if !ids[want] {
			t.Fatalf("研究线 %s 应保留可回看: %v", want, ids)
		}
	}
	// 版本选择按研究线各自独立。
	rw = env.do(http.MethodGet, "/api/versions?study=m1", "")
	if !strings.Contains(rw.Body.String(), `"version": 1`) {
		t.Fatalf("m1 应有自己的版本序列: %s", rw.Body.String())
	}
}
