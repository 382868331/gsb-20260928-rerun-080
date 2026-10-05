package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
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
	s.Mux.HandleFunc("/api/lines", s.handleLines)
	s.Mux.HandleFunc("/api/lines/state", s.handleLineState)
	s.Mux.HandleFunc("/api/lines/versions", s.handleLineVersions)
	s.Mux.HandleFunc("/api/lines/checkpoint", s.handleLineCheckpoint)
	s.Mux.HandleFunc("/api/lines/compare", s.handleLineCompare)
	s.Mux.HandleFunc("/api/lines/comparisons", s.handleLineComparisons)
	s.Mux.HandleFunc("/api/lines/merge", s.handleLineMerge)
	s.Mux.HandleFunc("/api/lines/withdraw", s.handleLineWithdraw)
}

// rebuild 依据当前全部区块、检查点与冲突裁决生成新版本快照并落盘。
func (s *Server) rebuild(label string) (model.VersionSnapshot, error) {
	next := int64(1)
	if latest, ok := s.Store.LatestSnapshot(); ok {
		next = latest.Version + 1
	}
	snap := chain.BuildSnapshot(s.Store.AllBlocks(), next, label, s.Store.Checkpoints(), s.Store.Resolutions())
	if err := s.Store.SaveSnapshot(snap); err != nil {
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
			snap = chain.BuildSnapshot(s.Store.AllBlocks(), 0, "", s.Store.Checkpoints(), s.Store.Resolutions())
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
	// LineID 非空时，本批区块同时计入该研究线的资料集合，
	// 并为该研究生成新的线版本（两位研究者各自导入互不干扰）。
	LineID string `json:"line_id,omitempty"`
}

type importResponse struct {
	Added     int                    `json:"added"`
	Skipped   int                    `json:"skipped"`
	State     model.VersionSnapshot  `json:"state"`
	LineState *model.VersionSnapshot `json:"line_state,omitempty"`
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
	var lineSnap *model.VersionSnapshot
	if req.LineID != "" {
		line, ok := s.Store.Line(req.LineID)
		if !ok {
			http.Error(w, "研究线 "+req.LineID+" 不存在", http.StatusNotFound)
			return
		}
		inLine := map[string]bool{}
		for _, h := range line.Blocks {
			inLine[h] = true
		}
		existingAdoption := map[string]bool{}
		for _, adoption := range line.Adoptions {
			existingAdoption[adoption.Hash] = true
		}
		var baseComparison *model.Comparison
		chosenSourceLines := map[string]bool{}
		for _, adoption := range line.Adoptions {
			if adoption.ComparisonID == line.BaseComparison && adoption.SourceLine != "both" {
				chosenSourceLines[adoption.SourceLine] = true
			}
		}
		if line.BaseComparison != "" {
			if cmp, ok := s.Store.Comparison(line.BaseComparison); ok {
				baseComparison = &cmp
			}
		}
		// 迟到父块归属：新导入区块若填补了某条已接纳待补充资料缺失的父块，
		// 继承该待补充块记录的出处（来源线与基础比较），使补交材料能够
		// 恢复先前接纳意图，而不是成为没有出处的孤立资料。
		lateParentSource := map[string]string{}
		if line.BaseComparison != "" {
			allByHash := map[string]model.Block{}
			for _, blk := range s.Store.AllBlocks() {
				allByHash[blk.Hash] = blk
			}
			adoptedSource := map[string]string{}
			for _, adoption := range line.Adoptions {
				if adoption.ComparisonID == line.BaseComparison {
					adoptedSource[adoption.Hash] = adoption.SourceLine
				}
			}
			newInBatch := map[string]bool{}
			for _, b := range req.Blocks {
				newInBatch[b.Hash] = true
			}
			// 先从已有采用决定寻找其缺失父块的出处。
			for childHash, source := range adoptedSource {
				child, ok := allByHash[childHash]
				if !ok || child.PrevHash == "" {
					continue
				}
				if newInBatch[child.PrevHash] {
					lateParentSource[child.PrevHash] = source
				}
			}
			// 同一批内多代父块互相填补：沿父指针在批次内传播出处。
			progress := true
			for progress {
				progress = false
				for _, b := range req.Blocks {
					if _, done := lateParentSource[b.Hash]; done || b.PrevHash == "" {
						continue
					}
					if src, ok := lateParentSource[b.PrevHash]; ok {
						lateParentSource[b.Hash] = src
						progress = true
					}
				}
			}
		}
		changed := false
		for _, b := range req.Blocks {
			if !inLine[b.Hash] {
				line.Blocks = append(line.Blocks, b.Hash)
				inLine[b.Hash] = true
				if line.BaseComparison != "" && baseComparison != nil && !existingAdoption[b.Hash] {
					sourceLine := "both"
					eligible := false
					// 优先：作为已接纳待补充块的迟到父块，继承其出处，
					// 使补交材料能够恢复先前接纳意图。
					if src, ok := lateParentSource[b.Hash]; ok {
						sourceLine = src
						eligible = true
					} else {
						for _, h := range baseComparison.OnlyA {
							if h == b.Hash && chosenSourceLines[baseComparison.LineA] {
								sourceLine = baseComparison.LineA
								eligible = true
							}
						}
						for _, h := range baseComparison.OnlyB {
							if h == b.Hash && chosenSourceLines[baseComparison.LineB] {
								sourceLine = baseComparison.LineB
								eligible = true
							}
						}
						for _, h := range baseComparison.Shared {
							if h == b.Hash {
								eligible = true
							}
						}
					}
					if eligible {
						line.Adoptions = append(line.Adoptions, model.Adoption{
							Hash:         b.Hash,
							ComparisonID: baseComparison.ID,
							SourceLine:   sourceLine,
							AdoptedAt:    time.Now().UTC().Format(time.RFC3339),
						})
						existingAdoption[b.Hash] = true
					}
				}
				changed = true
			}
		}
		if changed {
			sort.Strings(line.Blocks)
			if err := s.Store.UpdateLine(&line); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			snap, err := s.rebuildLine(&line, "导入 "+strings.TrimSpace(req.Label))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			lineSnap = &snap
		}
	}
	var snap model.VersionSnapshot
	if result.Added > 0 {
		snap, err = s.rebuild(strings.TrimSpace(req.Label))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("索引版本 %d 已生成: +%d 区块, 跳过 %d", snap.Version, result.Added, result.Skipped)
	} else {
		snap, _ = s.Store.LatestSnapshot()
	}
	writeJSON(w, importResponse{Added: result.Added, Skipped: result.Skipped, State: snap, LineState: lineSnap})
}

type checkpointRequest struct {
	Hash string `json:"hash"`
	Note string `json:"note"`
}

// handleCheckpoint 由研究者明确确认链检查点：对应区块及其之前的
// 历史范围成为确认范围。确认后重建索引版本，使各区域（拓扑、交易、
// 引用）在同一新版本中体现检查点；GET 返回全部已确认检查点。
func (s *Server) handleCheckpoint(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{"checkpoints": s.Store.Checkpoints()})
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
		if err := s.Store.AddCheckpoint(req.Hash, strings.TrimSpace(req.Note), confirmedAt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		snap, err := s.rebuild("确认检查点 " + req.Hash)
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
	if err := s.Store.ResolveConflict(req.ID, req.Action); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	snap, err := s.rebuild("冲突裁决 " + req.ID + " -> " + req.Action)
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
