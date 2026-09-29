package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

type Server struct{ store *Store }

func NewServer(store *Store) *Server { return &Server{store: store} }

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/health", s.health)
	mux.HandleFunc("/api/import", s.handleImport)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/versions", s.handleVersions)
	mux.HandleFunc("/api/blocks/", s.handleBlock)
	mux.HandleFunc("/api/transactions/", s.handleTransaction)
	mux.HandleFunc("/api/reset", s.handleReset)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	snap := s.store.Current()
	v := int64(0)
	if snap != nil {
		v = snap.Version
	}
	writeJSON(w, 200, map[string]any{"ok": true, "version": v, "node": "manual-only"})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var blocks []Block
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&blocks); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid block batch: "+err.Error())
		return
	}
	res := s.store.Import(blocks)
	writeJSON(w, 200, res)
}

func requestVersion(r *http.Request) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
	return v
}

func (s *Server) snapshotFor(w http.ResponseWriter, r *http.Request) *Snapshot {
	requested := requestVersion(r)
	snap := s.store.Version(requested)
	if snap == nil {
		writeErr(w, http.StatusGone, "version expired or unknown; reload the current version")
		return nil
	}
	w.Header().Set("X-Index-Version", strconv.FormatInt(snap.Version, 10))
	return snap
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshotFor(w, r)
	if snap == nil {
		return
	}
	writeJSON(w, 200, snap)
}

func (s *Server) handleVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"versions": s.store.Versions(), "current": s.store.Version(0).Version})
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshotFor(w, r)
	if snap == nil {
		return
	}
	hash := strings.TrimPrefix(r.URL.Path, "/api/blocks/")
	hash = strings.Split(hash, "/")[0]
	bv := snap.blockIndex[hash]
	if bv == nil {
		writeErr(w, http.StatusNotFound, "block not found in this version")
		return
	}
	writeJSON(w, 200, map[string]any{"version": snap.Version, "view": bv})
}

func (s *Server) handleTransaction(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshotFor(w, r)
	if snap == nil {
		return
	}
	hash := strings.TrimPrefix(r.URL.Path, "/api/transactions/")
	hash = strings.Split(hash, "/")[0]
	tv := snap.txIndex[hash]
	if tv == nil {
		writeErr(w, http.StatusNotFound, "transaction not found in this version")
		return
	}
	writeJSON(w, 200, map[string]any{"version": snap.Version, "view": tv})
}

// handleReset wipes local test data. It exists so the browser demo and tests
// can start from genesis; no network nodes are involved.
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	if err := resetStore(s.store); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
