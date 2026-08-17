package gateway

import (
	"context"
	"encoding/json"
	"flamerouter/internal/netutil"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var protectedSettingKeys = map[string]bool{
	"password":          true,
	"mitmSudoEncrypted": true,
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	settings, err := s.st.ListSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	out := make(map[string]any, len(settings)+4)

	for k, v := range settings {
		if protectedSettingKeys[k] {
			continue
		}

		out[k] = v
	}

	delete(out, "password")
	delete(out, "oidcClientSecret")

	_, hasPass := settings["password"]
	out["hasPassword"] = hasPass && settings["password"] != ""
	out["oidcConfigured"] = settings["oidcIssuerUrl"] != "" && settings["oidcClientId"] != "" && settings["oidcClientSecret"] != ""
	out["enableRequestLogs"] = os.Getenv("ENABLE_REQUEST_LOGS") == "true"
	out["enableTranslator"] = os.Getenv("ENABLE_TRANSLATOR") == "true"

	w.Header().Set("Cache-Control", "no-store")
	writeJSONOK(w, out)
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	for k := range protectedSettingKeys {
		delete(body, k)
	}
	// password change handled by /api/auth/reset-password; ignore newPassword here
	delete(body, "newPassword")
	delete(body, "currentPassword")

	for k, v := range body {
		var str string
		switch t := v.(type) {
		case string:
			str = t
		case nil:
			str = ""
		default:
			b, _ := json.Marshal(t) //nolint:errcheck // best-effort marshal
			str = string(b)
		}

		if err := s.st.SetSetting(k, str); err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}
	}

	s.handleGetSettings(w, r)
}

func (s *Server) handleRequireLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPatch {
		if err := s.patchRequireLogin(r); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	val, err := s.st.GetSetting("requireLogin")
	if err != nil {
		val = ""
	}
	// default true when unset (parity with 9router DEFAULT_SETTINGS)
	require := val == "" || val == "true" || val == "1"

	tunnelDash, err := s.st.GetSetting("tunnelDashboardAccess")
	if err != nil {
		tunnelDash = ""
	}

	tunnelURL, _ := s.st.GetSetting("tunnelUrl") //nolint:errcheck // best-effort lookup
	tsURL, _ := s.st.GetSetting("tailscaleUrl")  //nolint:errcheck // best-effort lookup

	tunnelDashOK := tunnelDash == "" || tunnelDash == "true" || tunnelDash == "1"
	writeJSONOK(w, map[string]any{
		"requireLogin":          require,
		"tunnelDashboardAccess": tunnelDashOK,
		"tunnelUrl":             tunnelURL,
		"tailscaleUrl":          tsURL,
	})
}

func (s *Server) patchRequireLogin(r *http.Request) error {
	var body struct {
		RequireLogin *bool `json:"requireLogin"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RequireLogin == nil {
		return fmt.Errorf("requireLogin required")
	}

	v := "false"
	if *body.RequireLogin {
		v = "true"
	}

	return s.st.SetSetting("requireLogin", v)
}

func parseProxyTestTimeout(timeoutMs int) time.Duration {
	timeout := 8 * time.Second

	if timeoutMs > 0 {
		ms := timeoutMs
		if ms > 30000 {
			ms = 30000
		}

		timeout = time.Duration(ms) * time.Millisecond
	}

	return timeout
}

func doProxyProbe(ctx context.Context, proxyURL, testURL string, timeout time.Duration) (int, string, int64, error) {
	transport := &http.Transport{ //nolint:exhaustruct // test transport
		Proxy: http.ProxyURL(mustParseURL(proxyURL)),
	}
	client := &http.Client{ //nolint:exhaustruct // test client
		Transport: transport,
		Timeout:   timeout,
	}
	started := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, testURL, nil)
	if err != nil {
		return 0, "", 0, err
	}

	req.Header.Set("User-Agent", "FlameRouter")

	res, err := netutil.DoHTTP(client, req)
	if err != nil {
		return 0, "", 0, err
	}
	defer res.Body.Close() //nolint:errcheck // cleanup body

	return res.StatusCode, res.Status, time.Since(started).Milliseconds(), nil
}

func (s *Server) handleProxyTest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProxyURL  string `json:"proxyUrl"`
		TestURL   string `json:"testUrl"`
		TimeoutMs int    `json:"timeoutMs"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	proxyURL := strings.TrimSpace(body.ProxyURL)
	if proxyURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "proxyUrl is required"})
		return
	}

	if _, err := url.Parse(proxyURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Invalid proxy URL: " + err.Error()})
		return
	}

	testURL := strings.TrimSpace(body.TestURL)
	if testURL == "" {
		testURL = "https://google.com/"
	}

	timeout := parseProxyTestTimeout(body.TimeoutMs)

	code, status, elapsed, err := doProxyProbe(r.Context(), proxyURL, testURL, timeout)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	writeJSONOK(w, map[string]any{
		"ok":         code >= 200 && code < 400,
		"status":     code,
		"statusText": status,
		"url":        testURL,
		"elapsedMs":  elapsed,
	})
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}

	return u
}

func (s *Server) handleDatabase(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetDatabase(w)
	case http.MethodPost:
		s.handlePostDatabase(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetDatabase(w http.ResponseWriter) {
	path := filepath.Clean(filepath.Join(s.cfg.DataDir, "flamerouter.db"))

	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to open database")
		return
	}

	defer f.Close() //nolint:errcheck // best-effort file close
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="flamerouter.db"`)

	if _, err := io.Copy(w, f); err != nil {
		_ = err
	}
}

func parseDatabaseUpload(r *http.Request) ([]byte, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			return nil, fmt.Errorf("invalid multipart")
		}

		file, _, err := r.FormFile("file")
		if err != nil {
			file, _, err = r.FormFile("database")
		}

		if err != nil {
			return nil, fmt.Errorf("file required")
		}

		defer file.Close() //nolint:errcheck // best-effort file close

		return io.ReadAll(file)
	}

	return io.ReadAll(io.LimitReader(r.Body, 64<<20))
}

func (s *Server) handlePostDatabase(w http.ResponseWriter, r *http.Request) {
	data, err := parseDatabaseUpload(r)
	if err != nil || len(data) < 16 {
		writeErr(w, http.StatusBadRequest, "invalid database payload")
		return
	}
	// SQLite magic header
	if string(data[:15]) != "SQLite format 3" {
		writeErr(w, http.StatusBadRequest, "not a sqlite database")
		return
	}

	path := filepath.Clean(filepath.Join(s.cfg.DataDir, "flamerouter.db"))
	tmp := path + ".restore-tmp"

	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, "write failed")
		return
	}
	// ponytail: live connections keep old handle until restart
	if err := os.Rename(tmp, path); err != nil {
		//nolint:errcheck // cleanup tmp
		_ = os.Remove(tmp)

		writeErr(w, http.StatusInternalServerError, "replace failed")

		return
	}

	writeJSONOK(w, map[string]any{"success": true, "note": "restart required for full effect"})
}
