package relay

import (
	"io/fs"
	"net/http"
	"os"
	"strings"
)

// StaticHandler serves the embedded SPA. Falls back to index.html for client-side routing.
func (s *Server) StaticHandler() http.Handler {
	// Check multiple possible locations for the built SPA
	for _, distDir := range []string{"web/dist", "/web/dist"} {
		if _, err := os.Stat(distDir); err == nil {
			fsys := os.DirFS(distDir)
			return spaHandler(http.FileServerFS(fsys), fsys)
		}
	}

	// Fallback: serve a simple message
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html><html><body style="background:#000;color:#0f0;font-family:monospace;padding:2em"><h1>phosphor</h1><p>Web UI not built. Run: cd web && npm ci && npm run build</p></body></html>`))
	})
}

// spaHandler wraps a file server to fall back to index.html for SPA routing.
func spaHandler(fileServer http.Handler, fsys fs.FS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Try to serve the file directly
		if _, err := fs.Stat(fsys, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// Fall back to index.html for client-side routing
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
