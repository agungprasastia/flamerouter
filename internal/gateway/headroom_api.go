package gateway

import (
	"encoding/json"
	"flamerouter/internal/config"
	"flamerouter/internal/infra/headroom"
	"flamerouter/internal/netutil"
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
		if v, err := s.st.GetSetting("headroomUrl"); err == nil && v != "" {
			u = v
		}
	}

	return u
}

func (s *Server) handleHeadroomStart(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	targetURL := s.headroomBaseURL()
	if err := headroomProc.Start(targetURL); err != nil {
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

	if err := headroomProc.Stop(); err != nil {
		_ = err
	}

	writeJSONOK(w, map[string]any{"success": true, "status": headroomProc.Status()})
}

func (s *Server) handleHeadroomRestart(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	targetURL := headroomProc.URL()
	if u := s.headroomBaseURL(); u != "" {
		targetURL = u
	}

	if err := headroomProc.Restart(targetURL); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "status": headroomProc.Status()})
		return
	}

	writeJSONOK(w, map[string]any{"success": true, "status": headroomProc.Status(), "url": headroomProc.URL()})
}

func (s *Server) handleHeadroomStatus(w http.ResponseWriter, _ *http.Request) {
	targetURL := s.headroomBaseURL()
	det := headroomProc.Detect()
	writeJSONOK(w, map[string]any{
		"status":  headroomProc.Status(),
		"url":     targetURL,
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
		s.handleInstallExtras(w, r)
	case http.MethodDelete:
		s.handleUninstallExtras(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

func (s *Server) handleExtrasAction(w http.ResponseWriter, r *http.Request, isInstall bool) {
	if !requireLocal(w, r) {
		return
	}

	var body struct {
		Extras []string `json:"extras"`
	}

	//nolint:errcheck
	_ = json.NewDecoder(r.Body).Decode(&body)

	var (
		result map[string]any
		err    error
	)

	failCode := "UNINSTALL_FAILED"

	if isInstall {
		failCode = "INSTALL_FAILED"
		result, err = headroomProc.InstallExtras(body.Extras)
	} else {
		result, err = headroomProc.UninstallExtras(body.Extras)
	}

	if err != nil {
		code := failCode
		status := http.StatusInternalServerError

		if e, ok := err.(interface{ Code() string }); ok {
			code = e.Code()
			if code == "NO_PYTHON" || code == "NOT_INSTALLED" || code == "INVALID_EXTRAS" {
				status = http.StatusBadRequest
			}
		}

		writeJSON(w, status, map[string]any{"error": err.Error(), "code": code})

		return
	}

	writeJSONOK(w, result)
}

func (s *Server) handleInstallExtras(w http.ResponseWriter, r *http.Request) {
	s.handleExtrasAction(w, r, true)
}

func (s *Server) handleUninstallExtras(w http.ResponseWriter, r *http.Request) {
	s.handleExtrasAction(w, r, false)
}

func buildHeadroomProxyRequest(r *http.Request, base *url.URL, path string) (*http.Request, error) {
	target := *base
	target.Path = "/" + strings.TrimPrefix(path, "/")
	target.RawQuery = r.URL.RawQuery

	var body io.Reader
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		body = r.Body
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), body)
	if err != nil {
		return nil, err
	}

	for k, vals := range r.Header {
		if hopByHopHeaders[strings.ToLower(k)] || strings.EqualFold(k, "Host") {
			continue
		}

		for _, v := range vals {
			outReq.Header.Add(k, v)
		}
	}

	sanitizeHeadroomProxyHeaders(outReq, base.Hostname())

	return outReq, nil
}

func sanitizeHeadroomProxyHeaders(req *http.Request, hostname string) {
	host := strings.Trim(hostname, "[]")
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		if !strings.EqualFold(host, "localhost") {
			req.Header.Del("Cookie")
			req.Header.Del("Authorization")
		}
	}

	req.Header.Del("Host")
}

func forwardHeadroomResponse(w http.ResponseWriter, res *http.Response, path string) {
	for k, vals := range res.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}

		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	if path == "dashboard" && strings.Contains(res.Header.Get("Content-Type"), "text/html") {
		serveHeadroomDashboardHTML(w, res)
		return
	}

	w.WriteHeader(res.StatusCode)

	if _, err := io.Copy(w, res.Body); err != nil {
		_ = err
	}
}

func serveHeadroomDashboardHTML(w http.ResponseWriter, res *http.Response) {
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		raw = nil
	}

	html := string(raw)
	html = strings.ReplaceAll(html, "fetch('/stats", "fetch('"+headroomProxyPrefix+"/stats")
	html = strings.ReplaceAll(html, "fetch('/health", "fetch('"+headroomProxyPrefix+"/health")
	html = strings.ReplaceAll(html, "fetch('/stats-history", "fetch('"+headroomProxyPrefix+"/stats-history")
	html = strings.ReplaceAll(html, "fetch('/transformations/feed", "fetch('"+headroomProxyPrefix+"/transformations/feed")

	w.Header().Del("Content-Length")
	w.WriteHeader(res.StatusCode)

	if _, err := io.WriteString(w, html); err != nil {
		_ = err
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

	outReq, err := buildHeadroomProxyRequest(r, base, path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	res, err := netutil.DoHTTP(http.DefaultClient, outReq)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	defer res.Body.Close() //nolint:errcheck // best-effort body close

	forwardHeadroomResponse(w, res, path)
}
