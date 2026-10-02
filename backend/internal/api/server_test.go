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

// 检查点测试数据：G->A1->A2->A3 主链；B1..B4 为越过检查点的迟到长分叉。
func checkpointTestBlocks() []model.Block {
	txX := model.Transaction{TxID: "txX", Inputs: []model.TxInput{{TxID: "coin-G", Vout: 0}}}
	txY := model.Transaction{TxID: "txY", Inputs: []model.TxInput{{TxID: "txX", Vout: 0}}}
	return []model.Block{
		{Hash: "G", PrevHash: "", Work: 100, Txs: []model.Transaction{{TxID: "coin-G"}}},
		{Hash: "A1", PrevHash: "G", Work: 10},
		{Hash: "A2", PrevHash: "A1", Work: 10, Txs: []model.Transaction{txX}},
		{Hash: "A3", PrevHash: "A2", Work: 10, Txs: []model.Transaction{txY}},
		{Hash: "B1", PrevHash: "G", Work: 20},
		{Hash: "B2", PrevHash: "B1", Work: 20, Txs: []model.Transaction{txX}},
		{Hash: "B3", PrevHash: "B2", Work: 20},
		{Hash: "B4", PrevHash: "B3", Work: 20},
	}
}

func TestCheckpointConflictAndResolution(t *testing.T) {
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
	stateOf := func(rw *httptest.ResponseRecorder) model.VersionSnapshot {
		t.Helper()
		var resp struct {
			State model.VersionSnapshot `json:"state"`
		}
		if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.State
	}
	importBlocks := func(blocks []model.Block, label string) model.VersionSnapshot {
		payload, _ := json.Marshal(map[string]any{"blocks": blocks, "label": label})
		rw := do(http.MethodPost, "/api/import", string(payload))
		if rw.Code != 200 {
			t.Fatalf("导入失败: %d %s", rw.Code, rw.Body.String())
		}
		return stateOf(rw)
	}

	all := checkpointTestBlocks()
	// 先导入主链部分并确认检查点 A2。
	st := importBlocks(all[:4], "主链")
	if st.TipHash != "A3" {
		t.Fatalf("初始主链端点应为 A3，实际 %s", st.TipHash)
	}
	// 只能确认主链区块：不存在的区块 404。
	if rw := do(http.MethodPost, "/api/checkpoint", `{"hash":"ZZ"}`); rw.Code != http.StatusNotFound {
		t.Fatalf("不存在区块应 404，实际 %d", rw.Code)
	}
	rw := do(http.MethodPost, "/api/checkpoint", `{"hash":"A2","note":"月度确认"}`)
	if rw.Code != 200 {
		t.Fatalf("确认检查点失败: %d %s", rw.Code, rw.Body.String())
	}
	st = stateOf(rw)
	if st.Checkpoint == nil || st.Checkpoint.Status != "ok" || st.Checkpoint.BlockHash != "A2" {
		t.Fatalf("检查点应已确认: %+v", st.Checkpoint)
	}
	if len(st.Checkpoint.Basis.Chain) != 3 || st.Checkpoint.Basis.Chain[2] != "A2" {
		t.Fatalf("确认依据应包含链根到检查点的链段: %+v", st.Checkpoint.Basis)
	}

	// 迟到长分叉越过检查点：主链钉住，冲突保留，确认不被自动撤销。
	st = importBlocks(all, "迟到长分叉")
	if st.TipHash != "A3" {
		t.Fatalf("冲突时主链应钉在 A3，实际 %s", st.TipHash)
	}
	if st.Checkpoint == nil || st.Checkpoint.Status != "conflict" || st.Checkpoint.Conflict.RivalTipHash != "B4" {
		t.Fatalf("应保留越过检查点的冲突: %+v", st.Checkpoint)
	}
	if st.Checkpoint.Conflict.RefChanges == nil || len(st.Checkpoint.Conflict.RefChanges.Entries) == 0 {
		t.Fatal("冲突应保留两种视角的引用差异")
	}

	// 重复导入同一批（含重复区块）：幂等，不产生新版本，冲突状态不变。
	st2 := importBlocks(all, "重放")
	if st2.Version != st.Version {
		t.Fatalf("重复导入不应产生新版本: v%d -> v%d", st.Version, st2.Version)
	}

	// 重开存储后，旧确认依据仍可追踪。
	s2, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mux2 := New(s2).Mux
	req := httptest.NewRequest(http.MethodGet, "/api/checkpoint", nil)
	rw2 := httptest.NewRecorder()
	mux2.ServeHTTP(rw2, req)
	if rw2.Code != 200 {
		t.Fatalf("重开后查询检查点失败: %d", rw2.Code)
	}
	var cpResp struct {
		Checkpoint *model.CheckpointView `json:"checkpoint"`
	}
	if err := json.Unmarshal(rw2.Body.Bytes(), &cpResp); err != nil {
		t.Fatal(err)
	}
	if cpResp.Checkpoint == nil || cpResp.Checkpoint.Basis.Version != 1 ||
		len(cpResp.Checkpoint.Basis.Chain) != 3 || cpResp.Checkpoint.Status != "conflict" {
		t.Fatalf("重开后旧确认依据应可追踪: %+v", cpResp.Checkpoint)
	}

	// 裁决一：建立独立研究线，原确认保留，主链仍钉住。
	rw = do(http.MethodPost, "/api/checkpoint/resolve", `{"action":"branch","label":"B 链独立研究"}`)
	if rw.Code != 200 {
		t.Fatalf("登记研究线失败: %d %s", rw.Code, rw.Body.String())
	}
	st = stateOf(rw)
	if st.TipHash != "A3" || st.Checkpoint == nil || st.Checkpoint.Status != "ok" {
		t.Fatalf("登记研究线后确认应保留且主链钉住: tip=%s %+v", st.TipHash, st.Checkpoint)
	}
	if len(st.ResearchLines) != 1 || st.ResearchLines[0].TipHash != "B4" {
		t.Fatalf("应登记 B4 为独立研究线: %+v", st.ResearchLines)
	}

	// 新的更重竞争链再次越过检查点：冲突再次出现。
	more := []model.Block{
		{Hash: "D1", PrevHash: "G", Work: 100},
		{Hash: "D2", PrevHash: "D1", Work: 100},
	}
	st = importBlocks(more, "又一条迟到分叉")
	if st.Checkpoint == nil || st.Checkpoint.Status != "conflict" || st.Checkpoint.Conflict.RivalTipHash != "D2" {
		t.Fatalf("新竞争链应再次形成冲突: %+v", st.Checkpoint)
	}

	// 裁决二：研究者显式采纳竞争链，原确认被显式撤销（非自动）。
	rw = do(http.MethodPost, "/api/checkpoint/resolve", `{"action":"adopt"}`)
	if rw.Code != 200 {
		t.Fatalf("采纳竞争链失败: %d %s", rw.Code, rw.Body.String())
	}
	st = stateOf(rw)
	if st.Checkpoint != nil {
		t.Fatalf("采纳后检查点确认应已撤销: %+v", st.Checkpoint)
	}
	if st.TipHash != "D2" {
		t.Fatalf("采纳后主链应为 D2，实际 %s", st.TipHash)
	}

	// 无冲突时裁决应 409；撤销不存在的检查点应 404。
	if rw := do(http.MethodPost, "/api/checkpoint/resolve", `{"action":"adopt"}`); rw.Code != http.StatusConflict {
		t.Fatalf("无冲突时裁决应 409，实际 %d", rw.Code)
	}
	if rw := do(http.MethodDelete, "/api/checkpoint", ""); rw.Code != http.StatusNotFound {
		t.Fatalf("无检查点时撤销应 404，实际 %d", rw.Code)
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
