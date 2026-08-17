package gateway

import (
	"context"
	"flamerouter/internal/mcp"
	"io"
	"net/http"
	"sync"
	"time"
)

var (
	mcpBridgeOnce sync.Once
	mcpBridge     = mcp.New()
)

func getMCPBridge() *mcp.Bridge {
	mcpBridgeOnce.Do(func() {
		if mcpBridge == nil {
			mcpBridge = mcp.New()
		}
	})

	return mcpBridge
}

func writeMCPHeartbeat(w io.Writer, flusher http.Flusher) {
	if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
		_ = err
	}

	flusher.Flush()
}

func writeMCPLine(w io.Writer, line []byte, flusher http.Flusher) {
	if _, err := io.WriteString(w, "data: "); err != nil {
		_ = err
	}

	if _, err := w.Write(line); err != nil {
		_ = err
	}

	if _, err := io.WriteString(w, "\n\n"); err != nil {
		_ = err
	}

	flusher.Flush()
}

func (s *Server) ensureMCPPluginRunning(plugin string, r *http.Request) error {
	br := getMCPBridge()
	if br.Running(plugin) {
		return nil
	}

	cmd := r.URL.Query().Get("command")
	if cmd == "" {
		return http.ErrNotSupported
	}

	return br.Start(plugin, cmd, r.URL.Query()["arg"])
}

// GET /api/mcp/{plugin}/sse — SSE stream from plugin.
func (s *Server) handleMCPSSE(w http.ResponseWriter, r *http.Request) {
	plugin := r.PathValue("plugin")
	if plugin == "" {
		writeErr(w, http.StatusBadRequest, "plugin required")
		return
	}

	if err := s.ensureMCPPluginRunning(plugin, r); err != nil {
		if err == http.ErrNotSupported {
			writeErr(w, http.StatusNotFound, "plugin not running")
		} else {
			writeErr(w, http.StatusInternalServerError, err.Error())
		}

		return
	}

	br := getMCPBridge()

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

	s.streamMCPEvents(r.Context(), w, flusher, ch)
}

func (s *Server) streamMCPEvents(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, ch <-chan []byte) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			writeMCPHeartbeat(w, flusher)
		case line, okChan := <-ch:
			if !okChan {
				return
			}

			writeMCPLine(w, line, flusher)
		}
	}
}

// POST /api/mcp/{plugin}/message — send message to plugin.
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
