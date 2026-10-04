package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"chainresearch/internal/chain"
	"chainresearch/internal/model"
	"chainresearch/internal/sample"
	"chainresearch/internal/store"
)

type Server struct {
	Store *store.Store
	Mux   *http.ServeMux
}

func New(s *store.Store) *Server {
	srv := &Server{Store: s, Mux: http.NewServeMux()}
	srv.routes()
	return srv
}

func (s *Server) routes() {
	s.Mux.HandleFunc("/api/state", s.handleState)
	s.Mux.HandleFunc("/api/versions", s.handleVersions)
	s.Mux.HandleFunc("/api/refdiff", s.handleRefDiff)
	s.Mux.HandleFunc("/api/import", s.handleImport)
	s.Mux.HandleFunc("/api/sample", s.handleSample)
	s.Mux.HandleFunc("/api/checkpoint", s.handleCheckpoint)
	s.Mux.HandleFunc("/api/conflicts/resolve", s.handleConflictResolve)
	s.Mux.HandleFunc("/api/studies", s.handleStudies)
	s.Mux.HandleFunc("/api/compare", s.handleCompare)
	s.Mux.HandleFunc("/api/merge", s.handleMerge)
	s.Mux.HandleFunc("/api/retract", s.handleRetract)
}

// studyOf 从查询参数或请求体字段取研究线 ID，缺省为默认研究线。
func studyOf(r *http.Request, body string) string {
	if body != "" {
		return body
	}
	if v := strings.TrimSpace(r.URL.Query().Get("study")); v != "" {
		return v
	}
	return store.DefaultStudy
}

// rebuild 依据指定研究线当前纳入的区块、检查点与冲突裁决
// 生成新版本快照并落盘。每条研究线的版本各自递增。
func (s *Server) rebuild(studyID, label string) (model.VersionSnapshot, error) {
	blocks, err := s.Store.StudyBlocks(studyID)
	if err != nil {
		return model.VersionSnapshot{}, err
	}
	next := int64(1)
	if latest, ok := s.Store.LatestSnapshotOf(studyID); ok {
		next = latest.Version + 1
	}
	snap := chain.BuildSnapshot(blocks, next, label, s.Store.CheckpointsOf(studyID), s.Store.ResolutionsOf(studyID))
	snap.Study = studyID
	if err := s.Store.SaveSnapshotTo(studyID, snap); err != nil {
		return model.VersionSnapshot{}, err
	}
	return snap, nil
}

type stateResponse struct {
	RequestedVersion *int64                `json:"requested_version,omitempty"`
	Snapshot         model.VersionSnapshot `json:"snapshot"`
}

// handleState 是页面唯一的数据来源：所有区块、交易位置、确认状态与
// 引用关系都属于返回快照的同一版本，避免前端拼出跨版本的不一致状态。
func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	studyID := studyOf(r, "")
	var requested *int64
	if v := r.URL.Query().Get("v"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			http.Error(w, "无效版本号", http.StatusBadRequest)
			return
		}
		requested = &n
	}
	var snap model.VersionSnapshot
	if requested != nil {
		var ok bool
		snap, ok = s.Store.SnapshotOf(studyID, *requested)
		if !ok {
			http.Error(w, "版本不存在", http.StatusNotFound)
			return
		}
	} else {
		latest, ok := s.Store.LatestSnapshotOf(studyID)
		if !ok {
			blocks, _ := s.Store.StudyBlocks(studyID)
			snap = chain.BuildSnapshot(blocks, 0, "", s.Store.CheckpointsOf(studyID), s.Store.ResolutionsOf(studyID))
			snap.Study = studyID
		} else {
			snap = latest
		}
	}
	writeJSON(w, stateResponse{RequestedVersion: requested, Snapshot: snap})
}

type versionMeta struct {
	Version   int64  `json:"version"`
	CreatedAt string `json:"created_at"`
	Label     string `json:"label,omitempty"`
	TipHash   string `json:"tip_hash"`
	TipHeight int64  `json:"tip_height"`
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	all := s.Store.VersionsOf(studyOf(r, ""))
	metas := make([]versionMeta, 0, len(all))
	for _, v := range all {
		metas = append(metas, versionMeta{
			Version: v.Version, CreatedAt: v.CreatedAt, Label: v.Label,
			TipHash: v.TipHash, TipHeight: v.TipHeight,
		})
	}
	writeJSON(w, map[string]any{"versions": metas})
}

// handleRefDiff 返回两个索引版本之间的引用差异，用于查看分叉切换
// 前后的引用变化：位置迁移、引用目标变化与仅确认数变化。
func (s *Server) handleRefDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	from, err := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	if err != nil {
		http.Error(w, "缺少有效的 from 版本号", http.StatusBadRequest)
		return
	}
	to, err := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	if err != nil {
		http.Error(w, "缺少有效的 to 版本号", http.StatusBadRequest)
		return
	}
	studyID := studyOf(r, "")
	oldSnap, ok := s.Store.SnapshotOf(studyID, from)
	if !ok {
		http.Error(w, "from 版本不存在", http.StatusNotFound)
		return
	}
	newSnap, ok := s.Store.SnapshotOf(studyID, to)
	if !ok {
		http.Error(w, "to 版本不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, chain.DiffRefs(oldSnap, newSnap))
}

type importRequest struct {
	Blocks []model.Block `json:"blocks"`
	Label  string        `json:"label"`
	Study  string        `json:"study"`
}

type importResponse struct {
	Added   int                   `json:"added"`
	Skipped int                   `json:"skipped"`
	State   model.VersionSnapshot `json:"state"`
}

// handleImport 幂等导入并在索引变化时生成新版本。
// 导入中断后重放：已索引区块全部跳过，若没有新区块则不产生新版本，
// 保证“继续”不会重复索引或产生空版本。
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Blocks) == 0 {
		http.Error(w, "blocks 不能为空", http.StatusBadRequest)
		return
	}
	studyID := studyOf(r, req.Study)
	result, err := s.Store.AddBlocksTo(studyID, req.Blocks)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	var snap model.VersionSnapshot
	if result.Added > 0 || result.StudyAdded > 0 {
		snap, err = s.rebuild(studyID, strings.TrimSpace(req.Label))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("索引版本 %d 已生成: +%d 区块, 跳过 %d", snap.Version, result.Added, result.Skipped)
	} else {
		snap, _ = s.Store.LatestSnapshotOf(studyID)
	}
	writeJSON(w, importResponse{Added: result.Added, Skipped: result.Skipped, State: snap})
}

type checkpointRequest struct {
	Hash  string `json:"hash"`
	Note  string `json:"note"`
	Study string `json:"study"`
}

// handleCheckpoint 由研究者明确确认链检查点：对应区块及其之前的
// 历史范围成为确认范围。确认后重建索引版本，使各区域（拓扑、交易、
// 引用）在同一新版本中体现检查点；GET 返回全部已确认检查点。
func (s *Server) handleCheckpoint(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"checkpoints": s.Store.CheckpointsOf(studyOf(r, ""))})
	case http.MethodPost:
		var req checkpointRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Hash) == "" {
			http.Error(w, "缺少检查点区块 hash", http.StatusBadRequest)
			return
		}
		confirmedAt := time.Now().UTC().Format(time.RFC3339)
		studyID := studyOf(r, req.Study)
		if err := s.Store.AddCheckpointTo(studyID, req.Hash, strings.TrimSpace(req.Note), confirmedAt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		snap, err := s.rebuild(studyID, "确认检查点 "+req.Hash)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("检查点已确认: %s（版本 %d）", req.Hash, snap.Version)
		writeJSON(w, map[string]any{"state": snap})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type conflictResolveRequest struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Study  string `json:"study"`
}

// handleConflictResolve 由研究者裁决越过检查点的冲突：
// action=independent 表示为该替代链建立独立研究。裁决只改变冲突的
// 标注状态，不会撤销已确认的检查点范围。
func (s *Server) handleConflictResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req conflictResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Action != "independent" {
		http.Error(w, "需要冲突 id 且 action 目前仅支持 independent", http.StatusBadRequest)
		return
	}
	studyID := studyOf(r, req.Study)
	if err := s.Store.ResolveConflictIn(studyID, req.ID, req.Action); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snap, err := s.rebuild(studyID, "冲突裁决 "+req.ID+" -> "+req.Action)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("冲突 %s 已裁决为 %s（版本 %d）", req.ID, req.Action, snap.Version)
	writeJSON(w, map[string]any{"state": snap})
}

func (s *Server) handleSample(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"blocks": sample.Blocks()})
}

// studySummary 是研究线列表中的条目：元数据加上最新版本要点，
// 供页面研究线选择器使用。
type studySummary struct {
	model.Study
	TipHash   string `json:"tip_hash"`
	TipHeight int64  `json:"tip_height"`
	Versions  int    `json:"versions"`
	Blocks    int    `json:"blocks"`
}

// handleStudies 列出全部研究线（GET）或创建空研究线（POST）。
// 合并生成的新线会记录来源；原有两条线及其确认依据保持可回看。
func (s *Server) handleStudies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		studies := s.Store.Studies()
		out := make([]studySummary, 0, len(studies))
		for _, meta := range studies {
			sum := studySummary{Study: meta}
			if snap, ok := s.Store.LatestSnapshotOf(meta.ID); ok {
				sum.TipHash = snap.TipHash
				sum.TipHeight = snap.TipHeight
				sum.Versions = len(s.Store.VersionsOf(meta.ID))
				sum.Blocks = len(snap.Blocks)
			}
			out = append(out, sum)
		}
		writeJSON(w, map[string]any{"studies": out})
	case http.MethodPost:
		var req struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		req.ID = strings.TrimSpace(req.ID)
		if req.ID == "" {
			http.Error(w, "缺少研究线 id", http.StatusBadRequest)
			return
		}
		meta, err := s.Store.CreateStudy(req.ID, strings.TrimSpace(req.Name), nil, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, map[string]any{"study": meta})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// latestOf 返回研究线最新快照；没有版本时按当前区块即时构建（版本 0）。
func (s *Server) latestOf(studyID string) (model.VersionSnapshot, error) {
	if snap, ok := s.Store.LatestSnapshotOf(studyID); ok {
		return snap, nil
	}
	blocks, err := s.Store.StudyBlocks(studyID)
	if err != nil {
		return model.VersionSnapshot{}, err
	}
	snap := chain.BuildSnapshot(blocks, 0, "", s.Store.CheckpointsOf(studyID), s.Store.ResolutionsOf(studyID))
	snap.Study = studyID
	return snap, nil
}

// handleCompare 对比两条研究线：共有事实（按区块 Hash 身份）、
// 各自独有的兼容后续 / 跨越确认边界的冲突 / 缺中间资料的断段，
// 以及各自独有的交易引用（同一交易在不同分叉的引用分别列出）。
func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	aID := strings.TrimSpace(r.URL.Query().Get("a"))
	bID := strings.TrimSpace(r.URL.Query().Get("b"))
	if aID == "" || bID == "" || aID == bID {
		http.Error(w, "需要两条不同的研究线 a 与 b", http.StatusBadRequest)
		return
	}
	snapA, err := s.latestOf(aID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	snapB, err := s.latestOf(bID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	blocksA, _ := s.Store.StudyBlocks(aID)
	blocksB, _ := s.Store.StudyBlocks(bID)
	writeJSON(w, chain.CompareStudies(snapA, snapB, blocksA, blocksB))
}

type mergeRequest struct {
	A              string   `json:"a"`
	B              string   `json:"b"`
	Name           string   `json:"name"`
	NewID          string   `json:"new_id"`
	Accept         []string `json:"accept"`
	Deferred       []string `json:"deferred"`
	KeepCheckpoint string   `json:"keep_checkpoint"` // "a" | "b" | ""
}

// handleMerge 合并两条研究线生成新线：新线 = 共有区块 ∪ 明确接纳的
// 后续区块；缺少的中间资料保留为待补充范围；未接纳的冲突部分记入
// 采用决定（deferred）而非标成无效。原有两条线保持不变。
func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req mergeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.A == "" || req.B == "" || req.A == req.B {
		http.Error(w, "需要两条不同的研究线 a 与 b", http.StatusBadRequest)
		return
	}
	newID := strings.TrimSpace(req.NewID)
	if newID == "" {
		newID = fmt.Sprintf("merge-%s-%s-%d", req.A, req.B, time.Now().UTC().Unix())
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = newID
	}
	meta, gaps, err := s.Store.MergeStudy(newID, name, req.A, req.B, req.Accept, req.KeepCheckpoint, req.Deferred)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	snap, err := s.rebuild(newID, fmt.Sprintf("合并 %s 与 %s", req.A, req.B))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	keptCp := ""
	if snap.Checkpoint != nil {
		keptCp = snap.Checkpoint.Hash
	}
	decision := model.Decision{
		ID:             fmt.Sprintf("d-%d", time.Now().UTC().UnixNano()),
		Kind:           "merge",
		Summary:        fmt.Sprintf("合并 %s 与 %s：接纳 %d 个后续区块，暂缓 %d 个冲突区块", req.A, req.B, len(req.Accept), len(req.Deferred)),
		FromStudies:    []string{req.A, req.B},
		Accepted:       req.Accept,
		Deferred:       req.Deferred,
		PendingGaps:    gaps,
		KeptCheckpoint: keptCp,
		Version:        snap.Version,
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.Store.AppendDecision(newID, decision); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	meta, _ = s.Store.StudyMeta(newID)
	log.Printf("研究线 %s 由 %s 与 %s 合并生成（版本 %d，待补充缺口 %d）", newID, req.A, req.B, snap.Version, len(gaps))
	writeJSON(w, map[string]any{"study": meta, "state": snap})
}

type retractRequest struct {
	Study string `json:"study"`
	Hash  string `json:"hash"`
}

// handleRetract 从研究线撤回一份未确认资料：检查点确认范围内的
// 区块不允许撤回；撤回后仍由其他区块独立提供的交易引用保持有效
// （同一交易在不同分叉的引用各自独立）。撤回生成新版本并记入
// 采用决定，重开后可复核。
func (s *Server) handleRetract(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req retractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	studyID := studyOf(r, req.Study)
	if strings.TrimSpace(req.Hash) == "" {
		http.Error(w, "缺少要撤回的区块 hash", http.StatusBadRequest)
		return
	}
	if err := s.Store.RetractBlock(studyID, req.Hash); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	snap, err := s.rebuild(studyID, "撤回未确认区块 "+req.Hash)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	decision := model.Decision{
		ID:        fmt.Sprintf("d-%d", time.Now().UTC().UnixNano()),
		Kind:      "retract",
		Summary:   fmt.Sprintf("撤回未确认区块 %s", req.Hash),
		Retracted: req.Hash,
		Version:   snap.Version,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.Store.AppendDecision(studyID, decision); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("研究线 %s 撤回区块 %s（版本 %d）", studyID, req.Hash, snap.Version)
	writeJSON(w, map[string]any{"state": snap})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
