package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flamerouter/internal/netutil"
	"flamerouter/internal/store"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	codexResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits" //nolint:gosec
	codexResetConsumeURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
)

func validateCodexConn(conn *store.Connection) (int, string) {
	if conn == nil {
		return http.StatusNotFound, "Connection not found"
	}

	if conn.Provider != "codex" {
		return http.StatusBadRequest, "Codex reset credits are only available for Codex connections."
	}

	if conn.AuthType != "oauth" && conn.AuthType != "access_token" {
		return http.StatusBadRequest, "Codex reset credits require an OAuth or access-token connection."
	}

	if conn.AccessToken == "" {
		return http.StatusUnauthorized, "No Codex access token available. Please re-authorize the connection."
	}

	return 0, ""
}

// GET|POST /api/usage/{connectionId}/codex-reset-credits.
func (s *Server) handleCodexResetCredits(w http.ResponseWriter, r *http.Request) {
	connID := r.PathValue("connectionId")
	if connID == "" {
		writeErr(w, http.StatusBadRequest, "connectionId required")
		return
	}

	conn, err := s.st.GetConnection(connID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	if code, errMsg := validateCodexConn(conn); code != 0 {
		writeJSON(w, code, map[string]any{"error": errMsg})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetCodexResetCredits(w, conn)
	case http.MethodPost:
		s.handlePostCodexResetCredits(w, conn)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGetCodexResetCredits(w http.ResponseWriter, conn *store.Connection) {
	result, err := getCodexRateLimitResetCredits(conn.AccessToken, conn.ProviderSpecificData)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSONOK(w, result)
}

func (s *Server) handlePostCodexResetCredits(w http.ResponseWriter, conn *store.Connection) {
	redeemID := randomUUID()
	result := consumeCodexRateLimitResetCredit(conn.AccessToken, redeemID)

	if result["ok"] == true {
		writeJSONOK(w, map[string]any{
			"code":            result["code"],
			"reset":           true,
			"windows_reset":   result["windowsReset"],
			"redeemRequestId": redeemID,
			"credit":          result["credit"],
		})

		return
	}

	if result["noCredit"] == true {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":          "no_credit",
			"reset":         false,
			"windows_reset": result["windowsReset"],
			"message":       "No Codex reset credits available.",
		})

		return
	}

	status := http.StatusBadGateway
	if st, ok := result["status"].(int); ok && st >= 400 && st < 500 {
		status = st
	}

	msg, _ := result["message"].(string) //nolint:errcheck // safe type assertion
	if msg == "" {
		msg = "Codex reset credit consume returned an unexpected response."
	}

	writeJSON(w, status, map[string]any{
		"code":          result["code"],
		"reset":         false,
		"windows_reset": result["windowsReset"],
		"message":       msg,
	})
}

func parseCodexCreditsList(creditsIn []any) []map[string]any {
	credits := make([]map[string]any, 0, len(creditsIn))

	for _, c := range creditsIn {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}

		status, _ := cm["status"].(string) //nolint:errcheck // safe type assertion
		if status == "" {
			status = "unknown"
		}

		credits = append(credits, map[string]any{
			"status":    status,
			"grantedAt": cm["granted_at"],
			"expiresAt": cm["expires_at"],
		})
	}

	return credits
}

func parseCodexAvailableCount(data map[string]any) int {
	avail := 0.0
	if v, ok := data["available_count"].(float64); ok {
		avail = v
	} else if v, ok := data["availableCount"].(float64); ok {
		avail = v
	}

	if avail < 0 {
		avail = 0
	}

	return int(avail)
}

func parseCodexErrorMessage(data map[string]any) string {
	if data == nil {
		return "Codex reset credits API unavailable"
	}

	for _, k := range []string{"message", "error", "detail"} {
		if v, ok := data[k].(string); ok && v != "" {
			return v
		}
	}

	return "Codex reset credits API unavailable"
}

func getCodexRateLimitResetCredits(accessToken string, psd map[string]any) (map[string]any, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, codexResetCreditsURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("OpenAI-Beta", "codex-1")
	req.Header.Set("originator", "codex_cli_rs")

	if accountID := codexAccountID(psd); accountID != "" {
		req.Header.Set("ChatGPT-Account-ID", accountID)
	}

	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       30 * time.Second,
	}

	res, err := netutil.DoHTTP(client, req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close() //nolint:errcheck // best-effort body close

	var data map[string]any
	//nolint:errcheck // best-effort decode
	_ = json.NewDecoder(res.Body).Decode(&data)

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, simpleError(parseCodexErrorMessage(data))
	}

	creditsIn, _ := data["credits"].([]any) //nolint:errcheck // safe type assertion
	credits := parseCodexCreditsList(creditsIn)
	availableCount := parseCodexAvailableCount(data)

	return map[string]any{"availableCount": availableCount, "credits": credits}, nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

func consumeCodexRateLimitResetCredit(accessToken, redeemRequestID string) map[string]any {
	payload, err := json.Marshal(map[string]string{"redeem_request_id": redeemRequestID})
	if err != nil {
		return map[string]any{"ok": false, "message": err.Error(), "status": 500}
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, codexResetConsumeURL, bytes.NewReader(payload))
	if err != nil {
		return map[string]any{"ok": false, "message": err.Error(), "status": 500}
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       30 * time.Second,
	}

	res, err := netutil.DoHTTP(client, req)
	if err != nil {
		return map[string]any{"ok": false, "message": err.Error(), "status": 500}
	}

	defer res.Body.Close() //nolint:errcheck // best-effort body close

	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		raw = nil
	}

	return parseCodexConsumeResponse(res.StatusCode, raw)
}

func parseCodexConsumeResponse(statusCode int, raw []byte) map[string]any {
	var data map[string]any

	if len(raw) > 0 {
		//nolint:errcheck // best-effort decode
		_ = json.Unmarshal(raw, &data)
	}

	code, _ := data["code"].(string) //nolint:errcheck // safe type assertion

	windowsReset := 0.0
	if v, ok := data["windows_reset"].(float64); ok {
		windowsReset = v
	}

	ok := statusCode >= 200 && statusCode < 300 && (code == "reset" || windowsReset > 0)
	noCredit := statusCode >= 200 && statusCode < 300 && code == "no_credit"
	msg, _ := data["message"].(string) //nolint:errcheck // safe type assertion

	var credit any
	if data != nil {
		credit = data["credit"]
	}

	return map[string]any{
		"ok":           ok,
		"noCredit":     noCredit,
		"status":       statusCode,
		"code":         code,
		"windowsReset": windowsReset,
		"message":      msg,
		"credit":       credit,
	}
}

func codexAccountID(psd map[string]any) string {
	if psd == nil {
		return ""
	}

	for _, k := range []string{"accountId", "workspaceId", "chatgptAccountId"} {
		if v, ok := psd[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}

	return ""
}

func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		_ = err
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])

	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
