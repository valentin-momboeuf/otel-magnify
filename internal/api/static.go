package api

import (
	"io/fs"
	"net/http"
	"strings"
)

// ServeStatic serves embedded frontend assets with SPA fallback to index.html.
func ServeStatic(fsys fs.FS) http.HandlerFunc {
	fileServer := http.FileServerFS(fsys)
	return func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		// Check if file exists; fall back to index.html for SPA routing
		if _, err := fs.Stat(fsys, path); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	}
}
