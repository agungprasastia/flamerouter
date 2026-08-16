package gateway

import (
	"encoding/json"
	"flamerouter/internal/config"
	"flamerouter/internal/infra/headroom"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var headroomProc = headroom.New()

const headroomProxyPrefix = "/api/headroom/proxy"

var hopByHopHeaders = map[string]bool{
	"connection": true, "keep-alive": true, "proxy-authenticate": true,
	"proxy-authorization": true, "te": true, "trailer": true,
	"transfer-encoding": true, "upgrade": true,
}

func (s *Server) headroomBaseURL() string {
	u := config.HeadroomURL()

	if s.st != nil {
		if v, _ := s.st.GetSetting("headroomUrl"); v != "" {
			u = v
		}
	}

	return u
}

func (s *Server) handleHeadroomStart(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	url := s.headroomBaseURL()
	if err := headroomProc.Start(url); err != nil {
		status := http.StatusInternalServerError
		if headroomProc.Status() == "not_installed" {
			status = http.StatusBadRequest
		}

		writeJSON(w, status, map[string]any{"error": err.Error(), "code": "NOT_INSTALLED", "status": headroomProc.Status()})

		return
	}

	writeJSONOK(w, map[string]any{"success": true, "status": headroomProc.Status(), "url": headroomProc.URL()})
}

func (s *Server) handleHeadroomStop(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	_ = headroomProc.Stop()
	writeJSONOK(w, map[string]any{"success": true, "status": headroomProc.Status()})
}

func (s *Server) handleHeadroomRestart(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	url := headroomProc.URL()
	if u := s.headroomBaseURL(); u != "" {
		url = u
	}

	if err := headroomProc.Restart(url); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "status": headroomProc.Status()})
		return
	}

	writeJSONOK(w, map[string]any{"success": true, "status": headroomProc.Status(), "url": headroomProc.URL()})
}

func (s *Server) handleHeadroomStatus(w http.ResponseWriter, r *http.Request) {
	url := s.headroomBaseURL()
	det := headroomProc.Detect()
	writeJSONOK(w, map[string]any{
		"status":  headroomProc.Status(),
		"url":     url,
		"healthy": headroomProc.Health(),
		"detect":  det,
	})
}

// GET|POST|DELETE /api/headroom/extras.
func (s *Server) handleHeadroomExtras(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("log") == "1" {
			writeJSONOK(w, map[string]any{"log": headroomProc.InstallLog()})
			return
		}

		writeJSONOK(w, headroomProc.ExtrasStatus())
	case http.MethodPost:
		if !requireLocal(w, r) {
			return
		}

		var body struct {
			Extras []string `json:"extras"`
		}

		_ = json.NewDecoder(r.Body).Decode(&body)

		result, err := headroomProc.InstallExtras(body.Extras)
		if err != nil {
			code := "INSTALL_FAILED"
			status := http.StatusInternalServerError

			if e, ok := err.(interface{ Code() string }); ok {
				code = e.Code()
				if code == "NO_PYTHON" || code == "NOT_INSTALLED" {
					status = http.StatusBadRequest
				}
			}

			writeJSON(w, status, map[string]any{"error": err.Error(), "code": code})

			return
		}

		writeJSONOK(w, result)
	case http.MethodDelete:
		if !requireLocal(w, r) {
			return
		}

		var body struct {
			Extras []string `json:"extras"`
		}

		_ = json.NewDecoder(r.Body).Decode(&body)

		result, err := headroomProc.UninstallExtras(body.Extras)
		if err != nil {
			code := "UNINSTALL_FAILED"
			status := http.StatusInternalServerError

			if e, ok := err.(interface{ Code() string }); ok {
				code = e.Code()
				if code == "NO_PYTHON" || code == "INVALID_EXTRAS" {
					status = http.StatusBadRequest
				}
			}

			writeJSON(w, status, map[string]any{"error": err.Error(), "code": code})

			return
		}

		writeJSONOK(w, result)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

// GET|POST|... /api/headroom/proxy/{path...} reverse proxy to headroom URL.
func (s *Server) handleHeadroomProxy(w http.ResponseWriter, r *http.Request) {
	baseStr := s.headroomBaseURL()

	base, err := url.Parse(baseStr)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Headroom URL must use http or https"})
		return
	}

	path := r.PathValue("path")
	target := *base
	target.Path = "/" + strings.TrimPrefix(path, "/")
	target.RawQuery = r.URL.RawQuery

	var body io.Reader
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		body = r.Body
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), body)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	for k, vals := range r.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}

		if strings.EqualFold(k, "Host") {
			continue
		}

		for _, v := range vals {
			outReq.Header.Add(k, v)
		}
	}

	host := strings.Trim(base.Hostname(), "[]")
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		if !strings.EqualFold(host, "localhost") {
			outReq.Header.Del("Cookie")
			outReq.Header.Del("Authorization")
		}
	}

	outReq.Header.Del("Host")

	res, err := http.DefaultClient.Do(outReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer res.Body.Close()

	for k, vals := range res.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}

		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	if path == "dashboard" && strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		raw, _ := io.ReadAll(res.Body)
		html := string(raw)
		html = strings.ReplaceAll(html, "fetch('/stats", "fetch('"+headroomProxyPrefix+"/stats")
		html = strings.ReplaceAll(html, "fetch('/health", "fetch('"+headroomProxyPrefix+"/health")
		html = strings.ReplaceAll(html, "fetch('/stats-history", "fetch('"+headroomProxyPrefix+"/stats-history")
		html = strings.ReplaceAll(html, "fetch('/transformations/feed", "fetch('"+headroomProxyPrefix+"/transformations/feed")

		w.Header().Del("Content-Length")
		w.WriteHeader(res.StatusCode)
		_, _ = io.WriteString(w, html)

		return
	}

	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}
