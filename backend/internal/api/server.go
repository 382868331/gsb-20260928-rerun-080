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
	s.Mux.HandleFunc("/api/checkpoint/resolve", s.handleCheckpointResolve)
}

// rebuild 以当前区块集合、检查点与研究线生成新版本快照并落盘。
func (s *Server) rebuild(label string) (model.VersionSnapshot, error) {
	next := int64(1)
	if latest, ok := s.Store.LatestSnapshot(); ok {
		next = latest.Version + 1
	}
	snap := chain.BuildSnapshotWithCheckpoint(s.Store.AllBlocks(), next, label, s.Store.Checkpoint(), s.Store.ResearchLines())
	if err := s.Store.SaveSnapshot(snap); err != nil {
		return snap, err
	}
	return snap, nil
}

// handleCheckpoint 管理研究者确认的链检查点。
// GET 返回当前检查点视图（含确认依据与冲突）与独立研究线；
// POST 确认一个主链区块为检查点；DELETE 显式撤销确认。
func (s *Server) handleCheckpoint(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var view *model.CheckpointView
		if latest, ok := s.Store.LatestSnapshot(); ok {
			view = latest.Checkpoint
		}
		writeJSON(w, map[string]any{
			"checkpoint":     view,
			"research_lines": s.Store.ResearchLines(),
		})
	case http.MethodPost:
		var req struct {
			Hash string `json:"hash"`
			Note string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		latest, ok := s.Store.LatestSnapshot()
		if !ok {
			http.Error(w, "尚未有任何索引版本，无法确认检查点", http.StatusConflict)
			return
		}
		var target *model.BlockView
		for i := range latest.Blocks {
			if latest.Blocks[i].Hash == req.Hash {
				target = &latest.Blocks[i]
			}
		}
		if target == nil {
			http.Error(w, "区块不存在于当前索引", http.StatusNotFound)
			return
		}
		if !target.Canonical {
			http.Error(w, "只能确认当前主链上的区块为检查点", http.StatusConflict)
			return
		}
		// 确认依据：当时版本、主链端点与链根到检查点的完整链段，
		// 持久化后重开仍可追踪旧确认依据。
		byHash := map[string]model.BlockView{}
		for _, b := range latest.Blocks {
			byHash[b.Hash] = b
		}
		chainHashes := []string{}
		for cur := target.Hash; ; {
			chainHashes = append([]string{cur}, chainHashes...)
			b := byHash[cur]
			if b.PrevHash == "" {
				break
			}
			if _, ok := byHash[b.PrevHash]; !ok {
				break
			}
			cur = b.PrevHash
		}
		cp := &model.Checkpoint{
			BlockHash:   target.Hash,
			Height:      target.Height,
			ConfirmedAt: time.Now().UTC().Format(time.RFC3339),
			Note:        strings.TrimSpace(req.Note),
			Basis: model.CheckpointBasis{
				Version:   latest.Version,
				TipHash:   latest.TipHash,
				TipHeight: latest.TipHeight,
				Chain:     chainHashes,
			},
		}
		if err := s.Store.SetCheckpoint(cp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		snap, err := s.rebuild("确认检查点 " + target.Hash)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("检查点已确认: %s @ #%d（依据版本 v%d）", cp.BlockHash, cp.Height, cp.Basis.Version)
		writeJSON(w, map[string]any{"state": snap})
	case http.MethodDelete:
		if s.Store.Checkpoint() == nil {
			http.Error(w, "当前没有已确认的检查点", http.StatusNotFound)
			return
		}
		if err := s.Store.ClearCheckpoint(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		snap, err := s.rebuild("撤销检查点确认")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("检查点确认已撤销")
		writeJSON(w, map[string]any{"state": snap})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCheckpointResolve 由研究者裁决越过检查点的冲突：
//   - action=adopt：显式撤销原确认，采纳竞争链（主链按工作量重选）；
//   - action=branch：保留原确认，把竞争链登记为独立研究线。
//
// 系统本身绝不自动撤销确认，两个动作都来自研究者的明确请求。
func (s *Server) handleCheckpointResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"`
		Label  string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体不是有效 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	latest, ok := s.Store.LatestSnapshot()
	if !ok || latest.Checkpoint == nil || latest.Checkpoint.Conflict == nil {
		http.Error(w, "当前没有待裁决的检查点冲突", http.StatusConflict)
		return
	}
	conflict := latest.Checkpoint.Conflict
	switch req.Action {
	case "adopt":
		if err := s.Store.ClearCheckpoint(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		label := strings.TrimSpace(req.Label)
		if label == "" {
			label = "采纳竞争链 " + conflict.RivalTipHash + "，撤销检查点确认"
		}
		snap, err := s.rebuild(label)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("冲突裁决：采纳竞争链 %s，原检查点确认已撤销", conflict.RivalTipHash)
		writeJSON(w, map[string]any{"state": snap})
	case "branch":
		label := strings.TrimSpace(req.Label)
		if label == "" {
			label = "独立研究：" + conflict.RivalTipHash
		}
		line := model.ResearchLine{
			ID:        "rl-" + conflict.RivalTipHash,
			Label:     label,
			TipHash:   conflict.RivalTipHash,
			TipHeight: conflict.RivalTipHeight,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := s.Store.AddResearchLine(line); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		snap, err := s.rebuild("登记独立研究线 " + line.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("冲突裁决：竞争链 %s 登记为独立研究线，原检查点确认保留", conflict.RivalTipHash)
		writeJSON(w, map[string]any{"state": snap})
	default:
		http.Error(w, "action 必须是 adopt 或 branch", http.StatusBadRequest)
	}
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
		snap, ok = s.Store.Snapshot(*requested)
		if !ok {
			http.Error(w, "版本不存在", http.StatusNotFound)
			return
		}
	} else {
		latest, ok := s.Store.LatestSnapshot()
		if !ok {
			snap = chain.BuildSnapshot(s.Store.AllBlocks(), 0, "")
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
	all := s.Store.Versions()
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
	oldSnap, ok := s.Store.Snapshot(from)
	if !ok {
		http.Error(w, "from 版本不存在", http.StatusNotFound)
		return
	}
	newSnap, ok := s.Store.Snapshot(to)
	if !ok {
		http.Error(w, "to 版本不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, chain.DiffRefs(oldSnap, newSnap))
}

type importRequest struct {
	Blocks []model.Block `json:"blocks"`
	Label  string        `json:"label"`
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
	result, err := s.Store.AddBlocks(req.Blocks)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	var snap model.VersionSnapshot
	if result.Added > 0 {
		next := int64(1)
		if latest, ok := s.Store.LatestSnapshot(); ok {
			next = latest.Version + 1
		}
		snap = chain.BuildSnapshotWithCheckpoint(s.Store.AllBlocks(), next, strings.TrimSpace(req.Label), s.Store.Checkpoint(), s.Store.ResearchLines())
		if err := s.Store.SaveSnapshot(snap); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("索引版本 %d 已生成: +%d 区块, 跳过 %d", next, result.Added, result.Skipped)
	} else {
		snap, _ = s.Store.LatestSnapshot()
	}
	writeJSON(w, importResponse{Added: result.Added, Skipped: result.Skipped, State: snap})
}

func (s *Server) handleSample(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]any{"blocks": sample.Blocks()})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
