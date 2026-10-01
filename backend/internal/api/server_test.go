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

func blockViewByHash(blocks []model.BlockView, hash string) *model.BlockView {
	for i := range blocks {
		if blocks[i].Hash == hash {
			return &blocks[i]
		}
	}
	return nil
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
	importBlocks := func(blocks []model.Block, label string) {
		payload, _ := json.Marshal(map[string]any{"blocks": blocks, "label": label})
		if rw := do(http.MethodPost, "/api/import", string(payload)); rw.Code != 200 {
			t.Fatalf("导入失败: %d %s", rw.Code, rw.Body.String())
		}
	}
	var oldChain []model.Block
	for _, b := range sample.Blocks() {
		if b.Hash == "G" || b.Hash[0] == 'A' {
			oldChain = append(oldChain, b)
		}
	}
	importBlocks(oldChain, "旧链")
	importBlocks(sample.Blocks(), "分叉切换")

	// 分叉切换前后的引用差异：txA 引用位置应从 A2 迁移到 B2。
	rw := do(http.MethodGet, "/api/refdiff?from=1&to=2", "")
	if rw.Code != 200 {
		t.Fatalf("refdiff 失败: %d %s", rw.Code, rw.Body.String())
	}
	var diff model.RefDiff
	if err := json.Unmarshal(rw.Body.Bytes(), &diff); err != nil {
		t.Fatal(err)
	}
	foundMigrate := false
	for _, e := range diff.Entries {
		if e.Kind == "position_migrated" && e.Before.FromTxID == "txA" &&
			e.Before.FromBlockHash == "A2" && e.After.FromBlockHash == "B2" {
			foundMigrate = true
		}
	}
	if !foundMigrate {
		t.Fatalf("应检出 txA 引用位置迁移: %+v", diff.Entries)
	}
	if rw := do(http.MethodGet, "/api/refdiff?from=1&to=99", ""); rw.Code != http.StatusNotFound {
		t.Fatalf("不存在版本应 404，实际 %d", rw.Code)
	}
	if rw := do(http.MethodGet, "/api/refdiff?from=1", ""); rw.Code != http.StatusBadRequest {
		t.Fatalf("缺少 to 应 400，实际 %d", rw.Code)
	}

	// 保存重开后：任一历史版本的引用（含区块/链视角/有效性）仍可追踪。
	s2, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux2 := New(s2).Mux
	for _, v := range []string{"1", "2"} {
		req := httptest.NewRequest(http.MethodGet, "/api/state?v="+v, nil)
		rw := httptest.NewRecorder()
		mux2.ServeHTTP(rw, req)
		if rw.Code != 200 {
			t.Fatalf("重开后版本 %s 应可用: %d", v, rw.Code)
		}
		var sr stateResponse
		if err := json.Unmarshal(rw.Body.Bytes(), &sr); err != nil {
			t.Fatal(err)
		}
		var txARefs []model.TxRef
		for _, r := range sr.Snapshot.TxRefs {
			if r.FromTxID == "txA" {
				txARefs = append(txARefs, r)
				if r.FromBlockHash == "" || r.FromBranchID == "" {
					t.Fatalf("版本 %s 的引用缺少区块/链视角信息: %+v", v, r)
				}
			}
		}
		if len(txARefs) == 0 {
			t.Fatalf("版本 %s 重开后应能追踪 txA 的引用", v)
		}
		if v == "1" && (len(txARefs) != 1 || !txARefs[0].Active || txARefs[0].FromBlockHash != "A2") {
			t.Fatalf("旧版本中 txA 引用应在 A2 且当前有效: %+v", txARefs)
		}
		if v == "2" {
			var active int
			for _, r := range txARefs {
				if r.Active {
					active++
					if r.FromBlockHash != "B2" {
						t.Fatalf("新版本中仅 B2 上的 txA 引用应当前有效: %+v", r)
					}
				}
			}
			if len(txARefs) != 2 || active != 1 {
				t.Fatalf("新版本中 txA 应有两条引用且仅一条当前有效: %+v", txARefs)
			}
		}
	}
}
