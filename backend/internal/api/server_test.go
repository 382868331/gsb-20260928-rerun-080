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
}

func blockViewByHash(blocks []model.BlockView, hash string) *model.BlockView {
	for i := range blocks {
		if blocks[i].Hash == hash {
			return &blocks[i]
		}
	}
	return nil
}
