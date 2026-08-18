package gateway

import (
	"flamerouter/internal/config"
	"flamerouter/internal/ops"
	"net/http"
	"runtime"
	"strings"
	"sync"
)

var (
	httpServerMu sync.RWMutex
	httpServer   *http.Server
)

// SetHTTPServer registers the running *http.Server for graceful shutdown.
func SetHTTPServer(srv *http.Server) {
	httpServerMu.Lock()
	httpServer = srv
	httpServerMu.Unlock()
}

func getHTTPServer() *http.Server {
	httpServerMu.RLock()
	defer httpServerMu.RUnlock()

	return httpServer
}

// GET /api/version.
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	current, latest, available, err := ops.CheckVersion()
	out := map[string]any{
		"version": ops.Version,
		"go":      runtime.Version(),
		"os":      runtime.GOOS,
		"arch":    runtime.GOARCH,
	}

	if err == nil {
		out["current"] = current
		out["latest"] = latest
		out["updateAvailable"] = available
	} else {
		out["current"] = ops.Version
		out["updateCheckError"] = err.Error()
	}

	writeJSONOK(w, out)
}

// POST /api/version/update.
func (s *Server) handleUpdate(w http.ResponseWriter, _ *http.Request) {
	if err := ops.SelfUpdate(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSONOK(w, map[string]string{"status": "updated"})
}

// POST /api/version/shutdown, POST /api/shutdown
// /api/shutdown requires SHUTDOWN_SECRET Bearer (parity 9router); /api/version/shutdown is open (dashboard).
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/api/shutdown") || r.URL.Path == "/api/shutdown" {
		secret := config.ShutdownSecret()
		if secret == "" || r.Header.Get("Authorization") != "Bearer "+secret {
			writeErr(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
	}

	writeJSONOK(w, map[string]string{"status": "shutting down"})

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		if err := ops.Shutdown(getHTTPServer()); err != nil {
			_ = err
		}
	}()
}
