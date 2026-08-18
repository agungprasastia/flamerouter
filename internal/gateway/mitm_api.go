package gateway

import (
	"encoding/json"
	"flamerouter/internal/infra/mitm"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

var (
	mitmMu     sync.Mutex
	mitmServer *mitm.Server
	mitmAPIKey string
)

func (s *Server) mitmCertPaths() (certPath, keyPath string) {
	dir := filepath.Join(os.TempDir(), "flamerouter-mitm")
	if s != nil && s.cfg != nil && s.cfg.DataDir != "" {
		dir = filepath.Join(s.cfg.DataDir, "mitm")
	}

	return filepath.Join(dir, "rootCA.crt"), filepath.Join(dir, "rootCA.key")
}

func (s *Server) getOrCreateMITM() (*mitm.Server, error) {
	mitmMu.Lock()
	defer mitmMu.Unlock()

	if mitmServer != nil {
		return mitmServer, nil
	}

	certPath, keyPath := s.mitmCertPaths()

	srv, err := mitm.New(certPath, keyPath)
	if err != nil {
		return nil, err
	}

	routerBase := "http://localhost:20128"
	if s != nil && s.cfg != nil && s.cfg.Port > 0 {
		routerBase = "http://localhost:" + strconv.Itoa(s.cfg.Port)
	}

	if s != nil && s.st != nil {
		if v, err := s.st.GetSetting("mitmRouterBaseUrl"); err == nil && v != "" {
			routerBase = v
		}
	}

	srv.RegisterDefaultTools(routerBase, mitmAPIKey)
	mitmServer = srv

	return mitmServer, nil
}

func (s *Server) mitmRouterBase() string {
	routerBase := "http://localhost:20128"
	if s.cfg != nil && s.cfg.Port > 0 {
		routerBase = "http://localhost:" + strconv.Itoa(s.cfg.Port)
	}

	if s.st != nil {
		if v, err := s.st.GetSetting("mitmRouterBaseUrl"); err == nil && v != "" {
			routerBase = v
		}
	}

	return routerBase
}

// POST /api/mitm/start.
func (s *Server) handleMITMStart(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	var body struct {
		APIKey string `json:"apiKey"`
		Addr   string `json:"addr"`
	}

	//nolint:errcheck // optional json body
	_ = json.NewDecoder(r.Body).Decode(&body)

	if body.APIKey != "" {
		mitmAPIKey = body.APIKey
	}

	addr := body.Addr
	if addr == "" {
		addr = ":443"
	}

	srv, err := s.getOrCreateMITM()
	if err != nil {
		log.Printf("[mitm] init failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to initialize mitm proxy"})

		return
	}

	srv.RegisterDefaultTools(s.mitmRouterBase(), mitmAPIKey)

	if err := srv.Start(addr); err != nil {
		log.Printf("[mitm] start failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to start mitm proxy", "status": srv.Status()})

		return
	}

	if s.st != nil {
		//nolint:errcheck // best-effort setting save
		_ = s.st.SetSetting("mitmEnabled", "true")
	}

	writeJSONOK(w, map[string]any{"success": true, "status": srv.Status(), "addr": addr})
}

// POST /api/mitm/stop.
func (s *Server) handleMITMStop(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	mitmMu.Lock()
	srv := mitmServer
	mitmMu.Unlock()

	if srv != nil {
		if err := srv.Stop(); err != nil {
			_ = err
		}
	}

	if s.st != nil {
		if err := s.st.SetSetting("mitmEnabled", "false"); err != nil {
			_ = err
		}
	}

	writeJSONOK(w, map[string]any{"success": true, "status": "stopped"})
}

// GET /api/mitm/status.
func (s *Server) handleMITMStatus(w http.ResponseWriter, _ *http.Request) {
	certPath, _ := s.mitmCertPaths()
	certExists := false

	if _, err := os.Stat(certPath); err == nil {
		certExists = true
	}

	status := "stopped"
	running := false
	certTrusted := false

	mitmMu.Lock()
	srv := mitmServer
	mitmMu.Unlock()

	if srv != nil {
		status = srv.Status()
		running = status == "running"
		certExists = certExists || srv.CertExists()

		if p := srv.CertPath(); p != "" {
			certTrusted = mitm.CheckCATrusted(p)
		}
	} else if certExists {
		certTrusted = mitm.CheckCATrusted(certPath)
	}

	writeJSONOK(w, map[string]any{
		"running":           running,
		"status":            status,
		"certExists":        certExists,
		"certTrusted":       certTrusted,
		"dnsStatus":         mitm.AllDNSStatus(),
		"isWin":             runtime.GOOS == "windows",
		"needsSudoPassword": runtime.GOOS != "windows",
		"hasCachedPassword": false,
	})
}

// GET /api/mitm/cert — download root CA PEM.
func (s *Server) handleMITMCert(w http.ResponseWriter, _ *http.Request) {
	srv, err := s.getOrCreateMITM()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	pem := srv.RootCAPEM()
	if len(pem) == 0 {
		// try disk
		certPath, _ := s.mitmCertPaths()
		pem, _ = os.ReadFile(filepath.Clean(certPath)) //nolint:gosec,errcheck // best-effort cert load
	}

	if len(pem) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "cert not found"})
		return
	}

	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="flamerouter-mitm-ca.crt"`)
	w.WriteHeader(http.StatusOK)
	//nolint:errcheck // write response
	_, _ = w.Write(pem)
}

// POST /api/mitm/trust — best-effort OS trust install.
func (s *Server) handleMITMTrust(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	srv, err := s.getOrCreateMITM()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	path := srv.CertPath()
	if path == "" {
		path, _ = s.mitmCertPaths()
	}

	if err := mitm.InstallCA(path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   err.Error(),
			"note":    "CA trust install is best-effort; run process elevated (Administrator/root)",
		})

		return
	}

	writeJSONOK(w, map[string]any{"success": true, "certTrusted": true})
}

// POST /api/mitm/hosts — enable/disable tool hosts file entries.
func (s *Server) handleMITMHosts(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	var body struct {
		Tool   string `json:"tool"`
		Action string `json:"action"` // enable | disable
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tool == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tool required"})
		return
	}

	var err error

	switch body.Action {
	case "disable":
		err = mitm.DisableToolHosts(body.Tool)
	default:
		err = mitm.EnableToolHosts(body.Tool)
	}

	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   err.Error(),
			"note":    "hosts file edit requires elevation (Administrator/root)",
		})

		return
	}
	// also update in-memory DNS override
	if srv, e := s.getOrCreateMITM(); e == nil {
		for _, h := range mitm.ToolHosts[body.Tool] {
			if body.Action == "disable" {
				srv.DNS().Delete(h)
			} else {
				srv.DNS().Set(h, "127.0.0.1")
			}
		}
	}

	writeJSONOK(w, map[string]any{"success": true, "dnsStatus": mitm.AllDNSStatus()})
}
