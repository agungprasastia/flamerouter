package gateway

import (
	"encoding/json"
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

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
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
	if _, ok := out["password"]; !ok {
		// strip already done
	}
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
			b, _ := json.Marshal(t)
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
		var body struct {
			RequireLogin *bool `json:"requireLogin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.RequireLogin == nil {
			writeErr(w, http.StatusBadRequest, "requireLogin required")
			return
		}
		v := "false"
		if *body.RequireLogin {
			v = "true"
		}
		if err := s.st.SetSetting("requireLogin", v); err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}
	}
	val, _ := s.st.GetSetting("requireLogin")
	// default true when unset (parity with 9router DEFAULT_SETTINGS)
	require := val == "" || val == "true" || val == "1"
	tunnelDash, _ := s.st.GetSetting("tunnelDashboardAccess")
	tunnelURL, _ := s.st.GetSetting("tunnelUrl")
	tsURL, _ := s.st.GetSetting("tailscaleUrl")
	tunnelDashOK := tunnelDash == "" || tunnelDash == "true" || tunnelDash == "1"
	writeJSONOK(w, map[string]any{
		"requireLogin":           require,
		"tunnelDashboardAccess":  tunnelDashOK,
		"tunnelUrl":              tunnelURL,
		"tailscaleUrl":           tsURL,
	})
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
	timeout := 8 * time.Second
	if body.TimeoutMs > 0 {
		ms := body.TimeoutMs
		if ms > 30000 {
			ms = 30000
		}
		timeout = time.Duration(ms) * time.Millisecond
	}
	// ponytail: no full ProxyAgent; HTTP_PROXY env for test client only
	transport := &http.Transport{Proxy: http.ProxyURL(mustParseURL(proxyURL))}
	client := &http.Client{Transport: transport, Timeout: timeout}
	started := time.Now()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodHead, testURL, nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	req.Header.Set("User-Agent", "FlameRouter")
	res, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = res.Body.Close()
	writeJSONOK(w, map[string]any{
		"ok":        res.StatusCode >= 200 && res.StatusCode < 400,
		"status":    res.StatusCode,
		"statusText": res.Status,
		"url":       testURL,
		"elapsedMs": time.Since(started).Milliseconds(),
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
		path := filepath.Join(s.cfg.DataDir, "flamerouter.db")
		f, err := os.Open(path)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "failed to open database")
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="flamerouter.db"`)
		_, _ = io.Copy(w, f)
	case http.MethodPost:
		// restore: prefer multipart file, else raw body
		var data []byte
		var err error
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err = r.ParseMultipartForm(64 << 20); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid multipart")
				return
			}
			file, _, ferr := r.FormFile("file")
			if ferr != nil {
				file, _, ferr = r.FormFile("database")
			}
			if ferr != nil {
				writeErr(w, http.StatusBadRequest, "file required")
				return
			}
			defer file.Close()
			data, err = io.ReadAll(file)
		} else {
			data, err = io.ReadAll(io.LimitReader(r.Body, 64<<20))
		}
		if err != nil || len(data) < 16 {
			writeErr(w, http.StatusBadRequest, "invalid database payload")
			return
		}
		// SQLite magic header
		if string(data[:15]) != "SQLite format 3" {
			writeErr(w, http.StatusBadRequest, "not a sqlite database")
			return
		}
		path := filepath.Join(s.cfg.DataDir, "flamerouter.db")
		tmp := path + ".restore-tmp"
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			writeErr(w, http.StatusInternalServerError, "write failed")
			return
		}
		// ponytail: live connections keep old handle until restart
		if err := os.Rename(tmp, path); err != nil {
			_ = os.Remove(tmp)
			writeErr(w, http.StatusInternalServerError, "replace failed")
			return
		}
		writeJSONOK(w, map[string]any{"success": true, "note": "restart required for full effect"})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
