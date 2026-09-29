package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
)

//go:embed all:dist
var distFS embed.FS

func main() {
	dir := os.Getenv("INDEX_DIR")
	if dir == "" {
		dir = "data"
	}
	store, err := OpenStore(dir)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	NewServer(store).Routes(mux)

	sub, err := fs.Sub(distFS, "dist")
	if err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			mux.Handle("/", spaHandler{http.FileServer(http.FS(sub))})
		}
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("manual chain index listening on %s (no public node connections)", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

type spaHandler struct{ h http.Handler }

func (s spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Let API paths miss explicitly; all other routes serve the SPA shell.
	if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
		http.NotFound(w, r)
		return
	}
	s.h.ServeHTTP(w, r)
}
