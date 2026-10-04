package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"chainresearch/internal/chain"
	"chainresearch/internal/model"
	"chainresearch/internal/store"
)

// rebuildLine 为研究线生成新版本快照并落盘；研究线有独立的版本序列，
// 页面的链、检查点、交易详情与版本选择器共同指向当前研究线的版本。
func (s *Server) rebuildLine(line *model.ResearchLine, label string) (model.VersionSnapshot, error) {
	next := int64(1)
	if latest, ok := s.Store.LatestLineSnapshot(line.ID); ok {
		next = latest.Version + 1
	}
	snap := chain.BuildLineSnapshot(line, s.Store.AllBlocks(), next, label, s.Store.Resolutions())
	if err := s.Store.SaveLineSnapshot(snap); err != nil {
		return model.VersionSnapshot{}, err
	}
	return snap, nil
}

type lineMeta struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	CreatedAt      string   `json:"created_at"`
	ParentLines    []string `json:"parent_lines,omitempty"`
	BaseComparison string   `json:"base_comparison,omitempty"`
	BlockCount     int      `json:"block_count"`
	Checkpoints    int      `json:"checkpoints"`
	LatestVersion  int64    `json:"latest_version"`
}

func toLineMeta(s *store.Store, line model.ResearchLine) lineMeta {
	m := lineMeta{
		ID: line.ID, Name: line.Name, CreatedAt: line.CreatedAt,
		ParentLines: line.ParentLines, BaseComparison: line.BaseComparison,
		BlockCount: len(line.Blocks), Checkpoints: len(line.Checkpoints),
	}
	if latest, ok := s.LatestLineSnapshot(line.ID); ok {
		m.LatestVersion = latest.Version
	}
	return m
}

// handleLines 列出全部研究线 / 创建新研究线。
// 原有两条线及其确认依据在合并后保持可回看，因此只增不改。
func (s *Server) handleLines(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		metas := []lineMeta{}
		for _, line := range s.Store.Lines() {
			metas = append(metas, toLineMeta(s.Store, line))
		}
		writeJSON(w, map[string]any{"lines": metas})
	case http.MethodPost:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		name := strings.TrimSpace(req.Name)
		if name == "" {
			http.Error(w, "缺少研究线名称", http.StatusBadRequest)
			return
		}
		line, err := s.Store.CreateLine(model.ResearchLine{
			Name:      name,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Blocks:    []string{},
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("研究线 %s（%s）已创建", line.ID, line.Name)
		writeJSON(w, map[string]any{"line": toLineMeta(s.Store, line)})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleLineState 返回研究线某版本的完整快照（缺省为最新版本）。
func (s *Server) handleLineState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lineID := r.URL.Query().Get("line")
	if _, ok := s.Store.Line(lineID); !ok {
		http.Error(w, "研究线不存在", http.StatusNotFound)
		return
	}
	if v := r.URL.Query().Get("v"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "无效版本号", http.StatusBadRequest)
			return
		}
		snap, ok := s.Store.LineSnapshot(lineID, n)
		if !ok {
			http.Error(w, "版本不存在", http.StatusNotFound)
			return
		}
		writeJSON(w, map[string]any{"snapshot": snap})
		return
	}
	snap, ok := s.Store.LatestLineSnapshot(lineID)
	if !ok {
		// 还没有任何版本（新建空线）：现算一个空视图，不产生版本。
		line, _ := s.Store.Line(lineID)
		snap = chain.BuildLineSnapshot(&line, s.Store.AllBlocks(), 0, "", s.Store.Resolutions())
	}
	writeJSON(w, map[string]any{"snapshot": snap})
}

func (s *Server) handleLineVersions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lineID := r.URL.Query().Get("line")
	if _, ok := s.Store.Line(lineID); !ok {
		http.Error(w, "研究线不存在", http.StatusNotFound)
		return
	}
	metas := []versionMeta{}
	for _, v := range s.Store.LineVersions(lineID) {
		metas = append(metas, versionMeta{
			Version: v.Version, CreatedAt: v.CreatedAt, Label: v.Label,
			TipHash: v.TipHash, TipHeight: v.TipHeight,
		})
	}
	writeJSON(w, map[string]any{"versions": metas})
}

// handleLineCheckpoint 在研究线上确认检查点：区块必须属于该研究线；
// 确认只影响该线，不会撤销其他研究线的确认决定。
func (s *Server) handleLineCheckpoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Line string `json:"line"`
		Hash string `json:"hash"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	line, ok := s.Store.Line(req.Line)
	if !ok {
		http.Error(w, "研究线不存在", http.StatusNotFound)
		return
	}
	inLine := false
	for _, h := range line.Blocks {
		if h == req.Hash {
			inLine = true
			break
		}
	}
	if !inLine {
		http.Error(w, "区块不属于该研究线，无法在此线上确认", http.StatusBadRequest)
		return
	}
	confirmedAt := time.Now().UTC().Format(time.RFC3339)
	updated := false
	for i := range line.Checkpoints {
		if line.Checkpoints[i].Hash == req.Hash {
			line.Checkpoints[i].Note = strings.TrimSpace(req.Note)
			line.Checkpoints[i].ConfirmedAt = confirmedAt
			updated = true
			break
		}
	}
	if !updated {
		line.Checkpoints = append(line.Checkpoints, model.Checkpoint{
			Hash: req.Hash, Note: strings.TrimSpace(req.Note), ConfirmedAt: confirmedAt,
		})
	}
	if err := s.Store.UpdateLine(&line); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snap, err := s.rebuildLine(&line, "确认检查点 "+req.Hash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("研究线 %s 检查点已确认: %s（线版本 %d）", line.ID, req.Hash, snap.Version)
	writeJSON(w, map[string]any{"state": snap})
}

// handleLineCompare 比较两条研究线并持久化比较结果：
// 共有事实、互相兼容的后续资料、跨越确认边界的冲突，
// 以及同一交易在两条线不同分叉里的出现位置。
func (s *Server) handleLineCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		A string `json:"a"`
		B string `json:"b"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.A == "" || req.B == "" || req.A == req.B {
		http.Error(w, "需要选择两条不同的研究线", http.StatusBadRequest)
		return
	}
	lineA, ok := s.Store.Line(req.A)
	if !ok {
		http.Error(w, "研究线 "+req.A+" 不存在", http.StatusNotFound)
		return
	}
	lineB, ok := s.Store.Line(req.B)
	if !ok {
		http.Error(w, "研究线 "+req.B+" 不存在", http.StatusNotFound)
		return
	}
	cmp := chain.CompareLines(&lineA, &lineB, s.Store.AllBlocks(), "")
	cmp, err := s.Store.AddComparison(cmp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("研究线比较 %s: %s vs %s（共有 %d，冲突 %d）",
		cmp.ID, cmp.LineA, cmp.LineB, len(cmp.Shared), len(cmp.Conflicts))
	writeJSON(w, map[string]any{"comparison": cmp})
}

func (s *Server) handleLineComparisons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cmps := s.Store.Comparisons()
	if cmps == nil {
		cmps = []model.Comparison{}
	}
	writeJSON(w, map[string]any{"comparisons": cmps})
}

// handleLineMerge 依据一次已持久化的比较合并两条研究线：
// 生成新研究线（原两线保持可回看），只接纳被选中的兼容后续资料，
// 跨越保留方确认边界的冲突部分记入暂缓而非判为无效。
func (s *Server) handleLineMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ComparisonID string   `json:"comparison_id"`
		Name         string   `json:"name"`
		Keep         string   `json:"keep"` // "a" 或 "b"
		Accept       []string `json:"accept"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	cmp, ok := s.Store.Comparison(req.ComparisonID)
	if !ok {
		http.Error(w, "比较不存在，请先比较两条研究线", http.StatusNotFound)
		return
	}
	lineA, _ := s.Store.Line(cmp.LineA)
	lineB, _ := s.Store.Line(cmp.LineB)
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "合并 " + lineA.Name + " + " + lineB.Name
	}
	merged, err := chain.MergeLines(&lineA, &lineB, cmp, chain.MergeSpec{
		Name:             name,
		KeepConfirmation: req.Keep,
		Accept:           req.Accept,
	}, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	created, err := s.Store.CreateLine(*merged)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snap, err := s.rebuildLine(&created, "合并 "+cmp.LineA+" + "+cmp.LineB+"（依据 "+cmp.ID+"）")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("研究线 %s 由 %s 与 %s 合并而成（依据 %s，线版本 %d）",
		created.ID, cmp.LineA, cmp.LineB, cmp.ID, snap.Version)
	writeJSON(w, map[string]any{"line": toLineMeta(s.Store, created), "state": snap})
}

// handleLineWithdraw 撤回研究线中一份未确认资料：
// 已确认范围（检查点及其之前）不允许撤回；撤回后重建线版本，
// 快照中每条引用的 independent_sources 标明哪些引用仍有独立来源。
func (s *Server) handleLineWithdraw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Line string `json:"line"`
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	line, ok := s.Store.Line(req.Line)
	if !ok {
		http.Error(w, "研究线不存在", http.StatusNotFound)
		return
	}
	inLine := false
	remaining := make([]string, 0, len(line.Blocks))
	for _, h := range line.Blocks {
		if h == req.Hash {
			inLine = true
			continue
		}
		remaining = append(remaining, h)
	}
	if !inLine {
		http.Error(w, "区块不属于该研究线", http.StatusBadRequest)
		return
	}
	// 已确认范围（生效检查点及其祖先）不允许撤回。
	idx := chain.NewBlockIndex(s.Store.AllBlocks())
	if cp := chain.EffectiveCheckpoint(&line, idx); cp != nil && idx.Descends(cp.Hash, req.Hash) {
		http.Error(w, "区块位于该研究线的已确认范围，不能撤回", http.StatusConflict)
		return
	}
	line.Blocks = remaining
	line.Withdrawals = append(line.Withdrawals, model.Withdrawal{
		Hash:        req.Hash,
		WithdrawnAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err := s.Store.UpdateLine(&line); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snap, err := s.rebuildLine(&line, "撤回未确认资料 "+req.Hash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("研究线 %s 已撤回未确认资料 %s（线版本 %d）", line.ID, req.Hash, snap.Version)
	writeJSON(w, map[string]any{"state": snap})
}
