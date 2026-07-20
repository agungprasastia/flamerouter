package gateway

import (
	"io"
	"net/http"
	"sync"
	"time"

	"flamerouter/internal/mcp"
)

var (
	mcpBridgeOnce sync.Once
	mcpBridge     *mcp.Bridge
)

func getMCPBridge() *mcp.Bridge {
	mcpBridgeOnce.Do(func() {
		mcpBridge = mcp.New()
	})
	return mcpBridge
}

// GET /api/mcp/{plugin}/sse — SSE stream from plugin
func (s *Server) handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	plugin := r.PathValue("plugin")
	if plugin == "" {
		writeErr(w, http.StatusBadRequest, "plugin required")
		return
	}
	br := getMCPBridge()
	// ensure process if command provided as query (optional lazy start)
	if !br.Running(plugin) {
		cmd := r.URL.Query().Get("command")
		if cmd == "" {
			writeErr(w, http.StatusNotFound, "plugin not running")
			return
		}
		if err := br.Start(plugin, cmd, r.URL.Query()["arg"]); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ch, err := br.Subscribe(plugin)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	defer br.Unsubscribe(plugin, ch)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// heartbeat ticker
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		case line, ok := <-ch:
			if !ok {
				return
			}
			_, _ = io.WriteString(w, "data: ")
			_, _ = w.Write(line)
			_, _ = io.WriteString(w, "\n\n")
			flusher.Flush()
		}
	}
}

// POST /api/mcp/{plugin}/message — send message to plugin
func (s *Server) handleMCPMessage(w http.ResponseWriter, r *http.Request) {
	plugin := r.PathValue("plugin")
	if plugin == "" {
		writeErr(w, http.StatusBadRequest, "plugin required")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := getMCPBridge().Send(plugin, body); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSONOK(w, map[string]any{"ok": true})
}
