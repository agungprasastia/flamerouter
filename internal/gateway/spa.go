package gateway

import (
	"io/fs"
	"net/http"
	"strings"

	"flamerouter/internal/gateway/ui"
)

func spaFileSystem() http.FileSystem {
	sub, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/") || strings.HasPrefix(p, "/v1/") ||
		strings.HasPrefix(p, "/v1beta/") || strings.HasPrefix(p, "/codex/") {
		http.NotFound(w, r)
		return
	}
	// Try file; if missing, index.html
	fsh := spaFileSystem()
	f, err := fsh.Open(strings.TrimPrefix(p, "/"))
	if err == nil {
		_ = f.Close()
		http.FileServer(fsh).ServeHTTP(w, r)
		return
	}
	// SPA fallback
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/"
	http.FileServer(fsh).ServeHTTP(w, r2)
}
