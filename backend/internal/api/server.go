package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

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
		snap = chain.BuildSnapshot(s.Store.AllBlocks(), next, strings.TrimSpace(req.Label))
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

// handleRefDiff 返回两个索引版本之间引用关系的差异，
// 用于对比分叉切换前后的位置迁移、引用目标变化与仅确认数变化。
func (s *Server) handleRefDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fromV, err := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	if err != nil {
		http.Error(w, "缺少或无效的 from 版本号", http.StatusBadRequest)
		return
	}
	toV, err := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	if err != nil {
		http.Error(w, "缺少或无效的 to 版本号", http.StatusBadRequest)
		return
	}
	fromSnap, ok := s.Store.Snapshot(fromV)
	if !ok {
		http.Error(w, "from 版本不存在", http.StatusNotFound)
		return
	}
	toSnap, ok := s.Store.Snapshot(toV)
	if !ok {
		http.Error(w, "to 版本不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, chain.DiffRefs(fromSnap, toSnap))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
