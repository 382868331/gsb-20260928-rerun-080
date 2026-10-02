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

// 完整流程：确认检查点 → 迟到长分叉越过检查点只记冲突 → 重复导入幂等 →
// 研究者裁决建立独立研究 → 重启后旧确认依据仍可追踪。
func TestCheckpointConflictFlow(t *testing.T) {
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
	var oldChain, full []model.Block
	full = sample.Blocks()
	for _, b := range full {
		if b.Hash == "G" || b.Hash[0] == 'A' {
			oldChain = append(oldChain, b)
		}
	}
	importBlocks := func(blocks []model.Block, label string) importResponse {
		payload, _ := json.Marshal(map[string]any{"blocks": blocks, "label": label})
		rw := do(http.MethodPost, "/api/import", string(payload))
		if rw.Code != 200 {
			t.Fatalf("导入失败: %d %s", rw.Code, rw.Body.String())
		}
		var ir importResponse
		if err := json.Unmarshal(rw.Body.Bytes(), &ir); err != nil {
			t.Fatal(err)
		}
		return ir
	}

	// v1：旧链为主链。
	ir := importBlocks(oldChain, "旧链")
	if ir.State.TipHash != "A3" {
		t.Fatalf("v1 主链端点应为 A3: %s", ir.State.TipHash)
	}

	// 确认检查点 A2 → 生成 v2，主链不变但出现确认范围。
	rw := do(http.MethodPost, "/api/checkpoint", `{"hash":"A2","note":"研究者确认"}`)
	if rw.Code != 200 {
		t.Fatalf("确认检查点失败: %d %s", rw.Code, rw.Body.String())
	}
	var cpResp struct {
		State model.VersionSnapshot `json:"state"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &cpResp); err != nil {
		t.Fatal(err)
	}
	if cpResp.State.Version != 2 || cpResp.State.Checkpoint == nil || cpResp.State.Checkpoint.Hash != "A2" {
		t.Fatalf("v2 应携带检查点 A2: %+v", cpResp.State.Checkpoint)
	}

	// 未索引区块不能确认为检查点。
	if rw := do(http.MethodPost, "/api/checkpoint", `{"hash":"ZZZ"}`); rw.Code != http.StatusBadRequest {
		t.Fatalf("未知区块应 400，实际 %d", rw.Code)
	}

	// v3：迟到长分叉 B 链工作量更高，但不得撤销 A2 确认，只记冲突。
	ir = importBlocks(full, "迟到长分叉")
	if ir.State.Version != 3 || ir.State.TipHash != "A3" {
		t.Fatalf("v3 主链端点应仍为 A3: tip=%s v=%d", ir.State.TipHash, ir.State.Version)
	}
	if len(ir.State.Conflicts) != 1 || ir.State.Conflicts[0].Status != "pending" {
		t.Fatalf("v3 应有一条待裁决冲突: %+v", ir.State.Conflicts)
	}
	conflictID := ir.State.Conflicts[0].ID
	scopeOf := func(snap model.VersionSnapshot, hash string) string {
		for _, b := range snap.Blocks {
			if b.Hash == hash {
				return b.Scope
			}
		}
		return ""
	}
	if scopeOf(ir.State, "A2") != model.ScopeConfirmed || scopeOf(ir.State, "A3") != model.ScopeTentative {
		t.Fatal("v3 中 A2 应为确认范围、A3 应为暂定范围")
	}
	if scopeOf(ir.State, "B4") != model.ScopeConflict {
		t.Fatal("v3 中 B 链应在冲突范围")
	}

	// 重复导入同一批数据（含重复区块）：幂等，不产生新版本。
	ir = importBlocks(full, "重放")
	if ir.Added != 0 || ir.State.Version != 3 {
		t.Fatalf("重放不应产生新版本: added=%d v=%d", ir.Added, ir.State.Version)
	}

	// 研究者裁决：为冲突链建立独立研究 → v4，确认范围仍不撤销。
	rw = do(http.MethodPost, "/api/conflicts/resolve", `{"id":"`+conflictID+`","action":"independent"}`)
	if rw.Code != 200 {
		t.Fatalf("裁决失败: %d %s", rw.Code, rw.Body.String())
	}
	var rr struct {
		State model.VersionSnapshot `json:"state"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &rr); err != nil {
		t.Fatal(err)
	}
	if rr.State.TipHash != "A3" || rr.State.Conflicts[0].Status != "independent" {
		t.Fatalf("v4 应保持原确认并标记 independent: %+v", rr.State.Conflicts)
	}

	// 重启后：检查点、裁决与历史版本的确认依据仍可追踪。
	s2, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux2 := New(s2).Mux
	do2 := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		rw := httptest.NewRecorder()
		mux2.ServeHTTP(rw, req)
		return rw
	}
	rw = do2("/api/checkpoint")
	if !strings.Contains(rw.Body.String(), "A2") {
		t.Fatalf("重启后检查点应仍可查询: %s", rw.Body.String())
	}
	for _, v := range []string{"2", "3", "4"} {
		req := httptest.NewRequest(http.MethodGet, "/api/state?v="+v, nil)
		rw := httptest.NewRecorder()
		mux2.ServeHTTP(rw, req)
		if rw.Code != 200 {
			t.Fatalf("重启后版本 %s 应可用: %d", v, rw.Code)
		}
		var sr stateResponse
		if err := json.Unmarshal(rw.Body.Bytes(), &sr); err != nil {
			t.Fatal(err)
		}
		if sr.Snapshot.Checkpoint == nil || sr.Snapshot.Checkpoint.Hash != "A2" {
			t.Fatalf("重启后版本 %s 应保留检查点依据", v)
		}
		if v == "3" && len(sr.Snapshot.Conflicts) != 1 {
			t.Fatalf("重启后版本 3 应保留冲突记录: %+v", sr.Snapshot.Conflicts)
		}
	}
}
