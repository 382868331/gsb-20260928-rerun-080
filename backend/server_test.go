package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestHTTPVersionConsistencyAndExpiry(t *testing.T) {
	s := newTestStore(t)
	mux := http.NewServeMux()
	NewServer(s).Routes(mux)
	do := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	post := func(blocks []Block) ImportResult {
		t.Helper()
		body := bytesReader(blocks)
		req := httptest.NewRequest(http.MethodPost, "/api/import", body)
		req.Header.Set("Content-Type", "application/json")
		w := do(req)
		var res ImportResult
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res
	}

	post([]Block{genesis("tx0")})
	r2 := post([]Block{mkBlock("A1", "G", 1, 1, "tx1")})
	oldV := r2.CurrentVersion
	post([]Block{mkBlock("B1", "G", 1, 9, "tx1", "tx2")})
	newV := s.Current().Version
	if newV == oldV {
		t.Fatal("version should advance after reorg")
	}

	// Pinned old version still reports tx1 confirmed on A1.
	w := do(httptest.NewRequest(http.MethodGet, "/api/state?version="+strconv.FormatInt(oldV, 10), nil))
	var oldSnap Snapshot
	json.Unmarshal(w.Body.Bytes(), &oldSnap)
	if oldSnap.Version != oldV || oldSnap.TipHash != "A1" {
		t.Fatalf("old snapshot changed: v=%d tip=%s", oldSnap.Version, oldSnap.TipHash)
	}

	// Detailed reads share the pinned version.
	w2 := do(httptest.NewRequest(http.MethodGet, "/api/transactions/tx1?version="+strconv.FormatInt(oldV, 10), nil))
	var detail map[string]any
	json.Unmarshal(w2.Body.Bytes(), &detail)
	if int64(detail["version"].(float64)) != oldV {
		t.Fatal("detail endpoint served a different version")
	}

	// Force expiry of all but the last 64 versions, then request an old one.
	for i := 0; i < keepVersions; i++ {
		post([]Block{mkBlock("o"+strconv.Itoa(1000+i), "missing"+strconv.Itoa(i), 1, 1)})
	}
	w3 := do(httptest.NewRequest(http.MethodGet, "/api/state?version=1", nil))
	if w3.Code != http.StatusGone {
		t.Fatalf("expected 410 for expired version, got %d", w3.Code)
	}
}

func bytesReader(blocks []Block) *bytes.Reader {
	body, _ := json.Marshal(blocks)
	return bytes.NewReader(body)
}
