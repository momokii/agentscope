package main

import (
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// newSPAHandler serves the embedded Vite build at /. Files that exist are
// served as-is; every other non-API path falls back to index.html so the
// hash-routed SPA boots on deep links and reloads. API routes never reach
// here — the top mux routes /api/ to the hub handler first.
func newSPAHandler(spa fs.FS) (http.Handler, error) {
	index, err := fs.ReadFile(spa, "index.html")
	if err != nil {
		return nil, fmt.Errorf("embedded SPA missing index.html: %w (rebuild with `pnpm --dir web build` before `go build`)", err)
	}
	files := http.FileServer(http.FS(spa))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if rel != "" {
			if st, err := fs.Stat(spa, rel); err == nil && !st.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(index)
	}), nil
}
