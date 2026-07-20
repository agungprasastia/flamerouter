package gateway

import (
	"net"
	"net/http"
	"strconv"

	"flamerouter/internal/infra/tunnel/cloudflare"
	"flamerouter/internal/infra/tunnel/tailscale"
)

// package-level managers (singleton process lifecycle)
var (
	cfTunnel = cloudflare.New()
	tsTunnel = tailscale.New()
)

func isLoopbackReq(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func requireLocal(w http.ResponseWriter, r *http.Request) bool {
	if isLoopbackReq(r) {
		return true
	}
	writeErr(w, http.StatusForbidden, "local only")
	return false
}

func gatewayPort(s *Server) int {
	if s != nil && s.cfg != nil && s.cfg.Port > 0 {
		return s.cfg.Port
	}
	return 20128
}

func (s *Server) handleTunnelEnable(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	port := gatewayPort(s)
	if err := cfTunnel.Enable(port); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "status": cfTunnel.Status()})
		return
	}
	writeJSONOK(w, map[string]any{"success": true, "status": cfTunnel.Status(), "url": cfTunnel.URL(), "port": port})
}

func (s *Server) handleTunnelDisable(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	_ = cfTunnel.Disable()
	writeJSONOK(w, map[string]any{"success": true, "status": cfTunnel.Status()})
}

func (s *Server) handleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	writeJSONOK(w, map[string]any{
		"tunnel": map[string]any{
			"status": cfTunnel.Status(),
			"url":    cfTunnel.URL(),
		},
		"tailscale": tsTunnel.Check(),
		"download":  map[string]any{"status": "idle"},
	})
}

func (s *Server) handleTailscaleInstall(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	if err := tsTunnel.Install(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "status": tsTunnel.Status()})
		return
	}
	writeJSONOK(w, map[string]any{"success": true, "status": tsTunnel.Status()})
}

func (s *Server) handleTailscaleEnable(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	port := gatewayPort(s)
	if v := r.URL.Query().Get("port"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			port = n
		}
	}
	if err := tsTunnel.Enable(port); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "status": tsTunnel.Status()})
		return
	}
	writeJSONOK(w, map[string]any{"success": true, "status": tsTunnel.Status(), "url": tsTunnel.URL(), "port": port})
}

func (s *Server) handleTailscaleDisable(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}
	_ = tsTunnel.Disable()
	writeJSONOK(w, map[string]any{"success": true, "status": tsTunnel.Status()})
}

func (s *Server) handleTailscaleCheck(w http.ResponseWriter, r *http.Request) {
	writeJSONOK(w, tsTunnel.Check())
}
