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

func TestImportFlowAndVersionConsistency(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	mux := New(s).Mux
	do := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		return rw
	}

	payload, _ := json.Marshal(map[string]any{"blocks": sample.Blocks(), "label": "完整分叉数据"})
	rw := do(http.MethodPost, "/api/import", string(payload))
	if rw.Code != 200 {
		t.Fatalf("导入失败: %d %s", rw.Code, rw.Body.String())
	}
	var ir importResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &ir); err != nil {
		t.Fatal(err)
	}
	if ir.State.Version != 1 || ir.State.TipHash != "B4" {
		t.Fatalf("导入后状态异常: tip=%s v=%d", ir.State.TipHash, ir.State.Version)
	}

	// 重复导入：幂等，不产生新版本。
	rw2 := do(http.MethodPost, "/api/import", string(payload))
	if err := json.Unmarshal(rw2.Body.Bytes(), &ir); err != nil {
		t.Fatal(err)
	}
	if ir.Added != 0 || ir.State.Version != 1 {
		t.Fatalf("重复导入不应产生新版本: added=%d v=%d", ir.Added, ir.State.Version)
	}

	// /api/state 中交易位置的状态必须与其所在区块同属一个版本。
	r3 := do(http.MethodGet, "/api/state", "")
	var sr stateResponse
	if err := json.Unmarshal(r3.Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	if sr.Snapshot.Version != 1 || sr.Snapshot.TipHash != "B4" {
		t.Fatalf("状态版本不一致: tip=%s v=%d", sr.Snapshot.TipHash, sr.Snapshot.Version)
	}
	for _, p := range sr.Snapshot.TxIndex {
		bv := blockViewByHash(sr.Snapshot.Blocks, p.BlockHash)
		if bv == nil || bv.Canonical != p.Canonical || bv.Confirmations != p.Confirmations {
			t.Fatalf("交易位置与区块状态跨版本不一致: %+v", p)
		}
	}

	if r4 := do(http.MethodGet, "/api/state?v=99", ""); r4.Code != http.StatusNotFound {
		t.Fatalf("不存在版本应 404，实际 %d", r4.Code)
	}
}

func TestEmptyStateIsVersionZero(t *testing.T) {
	s, _ := store.Open(filepath.Join(t.TempDir(), "data"))
	mux := New(s).Mux
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	rw := httptest.NewRecorder()
	mux.ServeHTTP(rw, req)
	var sr stateResponse
	json.Unmarshal(rw.Body.Bytes(), &sr)
	if sr.Snapshot.Version != 0 || len(sr.Snapshot.Blocks) != 0 {
		t.Fatalf("空库状态应为版本 0 且无区块: %+v", sr.Snapshot)
	}

	// 空快照的数组字段必须序列化为 [] 而非 null，否则前端首次打开会白屏。
	raw := rw.Body.String()
	for _, field := range []string{"\"blocks\": []", "\"tx_index\": []", "\"tx_refs\": []"} {
		if !strings.Contains(raw, field) {
			t.Fatalf("空快照字段应序列化为空数组，缺少 %s: %s", field, raw)
		}
	}
}

func TestRefDiffEndpointAndPersistence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux := New(s).Mux
	do := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		return rw
	}

	blocks := sample.Blocks()
	var oldChain []model.Block
	for _, b := range blocks {
		if b.Hash == "G" || b.Hash[0] == 'A' {
			oldChain = append(oldChain, b)
		}
	}
	p1, _ := json.Marshal(map[string]any{"blocks": oldChain, "label": "旧链"})
	if rw := do(http.MethodPost, "/api/import", string(p1)); rw.Code != 200 {
		t.Fatalf("导入旧链失败: %s", rw.Body.String())
	}
	p2, _ := json.Marshal(map[string]any{"blocks": blocks, "label": "分叉切换"})
	if rw := do(http.MethodPost, "/api/import", string(p2)); rw.Code != 200 {
		t.Fatalf("导入完整数据失败: %s", rw.Body.String())
	}

	// 重启后历史版本仍可按任一视角追踪引用。
	s2, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux2 := New(s2).Mux
	req := httptest.NewRequest(http.MethodGet, "/api/refdiff?from=1&to=2", nil)
	rw := httptest.NewRecorder()
	mux2.ServeHTTP(rw, req)
	if rw.Code != 200 {
		t.Fatalf("refdiff 失败: %d %s", rw.Code, rw.Body.String())
	}
	var diff model.RefDiff
	if err := json.Unmarshal(rw.Body.Bytes(), &diff); err != nil {
		t.Fatal(err)
	}
	if diff.FromVersion != 1 || diff.ToVersion != 2 || len(diff.Changes) == 0 {
		t.Fatalf("重启后应仍能计算版本间引用差异: %+v", diff)
	}
	kinds := map[string]bool{}
	for _, c := range diff.Changes {
		kinds[c.Kind] = true
	}
	if !kinds["confirmations"] || !kinds["added"] {
		t.Fatalf("分叉切换应产生确认数变化与新增引用: %+v", kinds)
	}

	// 重复导入不增加重复关系：v2 中同一花费位置只出现一次。
	req = httptest.NewRequest(http.MethodGet, "/api/state?v=2", nil)
	rw = httptest.NewRecorder()
	mux2.ServeHTTP(rw, req)
	var sr stateResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &sr); err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, r := range sr.Snapshot.TxRefs {
		seen[r.FromTxID+"@"+r.FromBlockHash+"->"+r.ToTxID]++
	}
	for k, n := range seen {
		if n != 1 {
			t.Fatalf("引用关系重复: %s 出现 %d 次", k, n)
		}
	}

	if rw := do(http.MethodGet, "/api/refdiff?from=1&to=99", ""); rw.Code != http.StatusNotFound {
		t.Fatalf("不存在版本应 404，实际 %d", rw.Code)
	}
}

func blockViewByHash(blocks []model.BlockView, hash string) *model.BlockView {
	for i := range blocks {
		if blocks[i].Hash == hash {
			return &blocks[i]
		}
	}
	return nil
}
