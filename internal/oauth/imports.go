package oauth

import (
	"encoding/json"
	"flamerouter/internal/store"
	"io"
	"net/http"
	"strings"
	"time"
)

// SpecializedImport dispatches POST/GET specialized oauth routes that store connections.
// path is after /api/oauth/ e.g. "cursor/import".
func (h *Handler) SpecializedImport(w http.ResponseWriter, r *http.Request, path string, st *store.Store) bool {
	switch path {
	case "cursor/import":
		h.importCursor(w, r, st)
	case "cursor/auto-import":
		h.autoImportStub(w, r, "cursor")
	case "codex/import-token":
		h.importCodexToken(w, r, st)
	case "codex/bulk-import":
		h.bulkImportCodex(w, r, st)
	case "iflow/cookie":
		h.importIFlowCookie(w, r, st)
	case "gitlab/pat":
		h.importGitLabPAT(w, r, st)
	case "kiro/import":
		h.importKiro(w, r, st)
	case "kiro/auto-import":
		h.autoImportStub(w, r, "kiro")
	case "kiro/api-key":
		h.importKiroAPIKey(w, r, st)
	case "kiro/import-cli-proxy":
		h.importKiroCLIProxy(w, r, st)
	case "kiro/social-authorize":
		h.kiroSocialAuthorize(w, r)
	case "kiro/social-exchange":
		h.kiroSocialExchange(w, r, st)
	default:
		return false
	}

	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func requirePOST(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return false
	}

	return true
}

func (h *Handler) autoImportStub(w http.ResponseWriter, r *http.Request, provider string) {
	// Local filesystem import not ported; clients fall back to manual import.
	writeJSON(w, http.StatusOK, map[string]any{
		"success":  false,
		"provider": provider,
		"error":    "auto-import not available; use manual import",
	})
}

func (h *Handler) importCursor(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	var req struct {
		AccessToken string `json:"accessToken"`
		MachineID   string `json:"machineId"`
		Name        string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AccessToken) == "" {
		writeErr(w, http.StatusBadRequest, "accessToken is required")
		return
	}

	if strings.TrimSpace(req.MachineID) == "" {
		writeErr(w, http.StatusBadRequest, "machineId is required")
		return
	}

	name := req.Name
	if name == "" {
		name = "Cursor Import"
	}

	psd := map[string]any{"machineId": strings.TrimSpace(req.MachineID), "authMethod": "import"}

	id, err := st.CreateOAuthConnection("cursor", "oauth", name, strings.TrimSpace(req.AccessToken), "", "", psd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": map[string]string{"id": id, "provider": "cursor"}})
}

func (h *Handler) importCodexToken(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	var req struct {
		AccessToken string `json:"accessToken"`
		Name        string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.AccessToken) == "" {
		writeErr(w, http.StatusBadRequest, "accessToken is required")
		return
	}

	token := strings.TrimSpace(req.AccessToken)

	name := req.Name
	if name == "" {
		name = "ChatGPT Access Token"
	}

	psd := map[string]any{"authMethod": "access_token"}

	id, err := st.CreateOAuthConnection("codex", "access_token", name, token, "", "", psd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": map[string]string{"id": id, "provider": "codex"}})
}

func (h *Handler) bulkImportCodex(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	var accounts []map[string]any

	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				accounts = append(accounts, m)
			}
		}
	case map[string]any:
		if arr, ok := v["accounts"].([]any); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					accounts = append(accounts, m)
				}
			}
		} else {
			accounts = append(accounts, v)
		}
	}

	if len(accounts) == 0 {
		writeErr(w, http.StatusBadRequest, "no accounts provided")
		return
	}

	var results []map[string]any

	okN, failN := 0, 0

	for i, acc := range accounts {
		tok, _ := acc["accessToken"].(string)
		if tok == "" {
			tok, _ = acc["access_token"].(string)
		}

		if strings.TrimSpace(tok) == "" {
			failN++

			results = append(results, map[string]any{"index": i, "success": false, "error": "accessToken required"})

			continue
		}

		name, _ := acc["name"].(string)
		if name == "" {
			name = "Codex Bulk"
		}

		rt, _ := acc["refreshToken"].(string)
		if rt == "" {
			rt, _ = acc["refresh_token"].(string)
		}

		psd := map[string]any{"authMethod": "bulk_import"}

		id, err := st.CreateOAuthConnection("codex", "oauth", name, strings.TrimSpace(tok), strings.TrimSpace(rt), "", psd)
		if err != nil {
			failN++

			results = append(results, map[string]any{"index": i, "success": false, "error": err.Error()})

			continue
		}

		okN++

		results = append(results, map[string]any{"index": i, "success": true, "id": id})
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": okN, "failed": failN, "results": results})
}

func (h *Handler) importIFlowCookie(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	var req struct {
		Cookie string `json:"cookie"`
		Name   string `json:"name"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Cookie) == "" {
		writeErr(w, http.StatusBadRequest, "cookie is required")
		return
	}

	c := strings.TrimSpace(req.Cookie)
	if !strings.Contains(c, "BXAuth=") {
		writeErr(w, http.StatusBadRequest, "cookie must contain BXAuth field")
		return
	}

	if !strings.HasSuffix(c, ";") {
		c += ";"
	}

	name := req.Name
	if name == "" {
		name = "iFlow Cookie"
	}

	psd := map[string]any{"cookie": c, "authMethod": "cookie"}

	id, err := st.CreateOAuthConnection("iflow", "cookie", name, c, "", "", psd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": map[string]string{"id": id, "provider": "iflow"}})
}

func (h *Handler) importGitLabPAT(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	var req struct {
		Token   string `json:"token"`
		BaseURL string `json:"baseUrl"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		writeErr(w, http.StatusBadRequest, "personal access token is required")
		return
	}

	base := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if base == "" {
		base = "https://gitlab.com"
	}

	name := "GitLab PAT"
	psd := map[string]any{"baseUrl": base, "authKind": "personal_access_token"}

	id, err := st.CreateOAuthConnection("gitlab", "oauth", name, strings.TrimSpace(req.Token), "", "", psd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": map[string]string{"id": id, "provider": "gitlab"}})
}

func (h *Handler) importKiro(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	var req struct {
		RefreshToken string `json:"refreshToken"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		Region       string `json:"region"`
		AuthMethod   string `json:"authMethod"`
		ProfileArn   string `json:"profileArn"`
		AccessToken  string `json:"accessToken"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		writeErr(w, http.StatusBadRequest, "refreshToken is required")
		return
	}

	psd := map[string]any{"authMethod": "imported", "provider": "Imported"}

	if req.ClientID != "" && req.ClientSecret != "" {
		region := req.Region
		if region == "" {
			region = "us-east-1"
		}

		psd = map[string]any{
			"clientId": req.ClientID, "clientSecret": req.ClientSecret,
			"region": region, "authMethod": "idc", "provider": "Enterprise",
		}
	}

	if req.ProfileArn != "" {
		psd["profileArn"] = req.ProfileArn
	}

	access := strings.TrimSpace(req.AccessToken)
	refresh := strings.TrimSpace(req.RefreshToken)
	// Best-effort live refresh; store refresh even if refresh fails (offline import).
	if tok, err := RefreshKiroToken(r.Context(), refresh, psd); err == nil && tok != nil {
		access = tok.AccessToken

		if tok.RefreshToken != "" {
			refresh = tok.RefreshToken
		}
	}

	exp := ""
	if access != "" {
		exp = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	}

	id, err := st.CreateOAuthConnection("kiro", "oauth", "Kiro Import", access, refresh, exp, psd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": map[string]string{"id": id, "provider": "kiro"}})
}

func (h *Handler) importKiroAPIKey(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	var req struct {
		APIKey string `json:"apiKey"`
		Region string `json:"region"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.APIKey) == "" {
		writeErr(w, http.StatusBadRequest, "apiKey is required")
		return
	}

	region := req.Region
	if region == "" {
		region = "us-east-1"
	}

	key := strings.TrimSpace(req.APIKey)
	psd := map[string]any{"region": region, "authMethod": "api_key", "provider": "API Key"}
	exp := time.Now().Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339)

	id, err := st.CreateOAuthConnection("kiro", "api_key", "Kiro API Key", key, "", exp, psd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": map[string]string{"id": id, "provider": "kiro"}})
}

func (h *Handler) importKiroCLIProxy(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	raw := body

	for _, k := range []string{"cliProxyAuth", "auth", "json"} {
		if m, ok := body[k].(map[string]any); ok {
			raw = m
			break
		}
	}

	access, _ := raw["accessToken"].(string)
	if access == "" {
		access, _ = raw["access_token"].(string)
	}

	refresh, _ := raw["refreshToken"].(string)
	if refresh == "" {
		refresh, _ = raw["refresh_token"].(string)
	}

	if access == "" && refresh == "" {
		writeErr(w, http.StatusBadRequest, "cli proxy auth requires accessToken or refreshToken")
		return
	}

	psd := map[string]any{"authMethod": "external_idp", "provider": "CLIProxy"}
	if p, ok := raw["providerSpecificData"].(map[string]any); ok {
		psd = p
	}

	exp, _ := raw["expiresAt"].(string)

	id, err := st.CreateOAuthConnection("kiro", "oauth", "Kiro CLI Proxy", access, refresh, exp, psd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "connection": map[string]string{"id": id, "provider": "kiro"}})
}

func (h *Handler) kiroSocialAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	provider := r.URL.Query().Get("provider")
	if provider != "google" && provider != "github" {
		writeErr(w, http.StatusBadRequest, "provider must be google or github")
		return
	}

	verifier, err := GenerateCodeVerifier()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "pkce failed")
		return
	}

	state, err := GenerateState()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "state failed")
		return
	}

	challenge := GenerateCodeChallenge(verifier)
	// Kiro social login via desktop auth host (parity URL shape).
	authURL := "https://prod.us-east-1.auth.desktop.kiro.dev/login?idp=" + provider +
		"&code_challenge=" + challenge + "&code_challenge_method=S256&state=" + state
	writeJSON(w, http.StatusOK, map[string]string{
		"authUrl":       authURL,
		"state":         state,
		"codeVerifier":  verifier,
		"codeChallenge": challenge,
		"provider":      provider,
	})
}

func (h *Handler) kiroSocialExchange(w http.ResponseWriter, r *http.Request, st *store.Store) {
	if !requirePOST(w, r) {
		return
	}

	var req struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"codeVerifier"`
		Provider     string `json:"provider"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" || req.CodeVerifier == "" {
		writeErr(w, http.StatusBadRequest, "code and codeVerifier required")
		return
	}

	if req.Provider != "google" && req.Provider != "github" {
		writeErr(w, http.StatusBadRequest, "invalid provider")
		return
	}
	// Store code as pending oauth connection; full token exchange needs live Kiro IdP.
	// Accept body tokens if client already exchanged.
	psd := map[string]any{"authMethod": req.Provider, "provider": strings.ToUpper(req.Provider[:1]) + req.Provider[1:], "codeVerifier": req.CodeVerifier}

	id, err := st.CreateOAuthConnection("kiro", "oauth", "Kiro "+req.Provider, req.Code, "", "", psd)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"connection": map[string]string{"id": id, "provider": "kiro"},
		// ponytail: code stored as access until live social token exchange wired
		"note": "stored authorization code; complete exchange when Kiro IdP client available",
	})
}
