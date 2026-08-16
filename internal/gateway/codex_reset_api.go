package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flamerouter/internal/netutil"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	codexResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	codexResetConsumeURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
)

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

	if conn == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "Connection not found"})
		return
	}

	if conn.Provider != "codex" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Codex reset credits are only available for Codex connections."})
		return
	}

	if conn.AuthType != "oauth" && conn.AuthType != "access_token" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Codex reset credits require an OAuth or access-token connection."})
		return
	}

	if conn.AccessToken == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "No Codex access token available. Please re-authorize the connection."})
		return
	}

	switch r.Method {
	case http.MethodGet:
		result, err := getCodexRateLimitResetCredits(conn.AccessToken, conn.ProviderSpecificData)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSONOK(w, result)
	case http.MethodPost:
		redeemID := randomUUID()
		result := consumeCodexRateLimitResetCredit(conn.AccessToken, redeemID)

		if result["ok"] == true {
			writeJSONOK(w, map[string]any{
				"code": result["code"], "reset": true,
				"windows_reset":   result["windowsReset"],
				"redeemRequestId": redeemID,
				"credit":          result["credit"],
			})

			return
		}

		if result["noCredit"] == true {
			writeJSON(w, http.StatusConflict, map[string]any{
				"code": "no_credit", "reset": false,
				"windows_reset": result["windowsReset"],
				"message":       "No Codex reset credits available.",
			})

			return
		}

		status := http.StatusBadGateway
		if st, ok := result["status"].(int); ok && st >= 400 && st < 500 {
			status = st
		}

		msg, _ := result["message"].(string)
		if msg == "" {
			msg = "Codex reset credit consume returned an unexpected response."
		}

		writeJSON(w, status, map[string]any{
			"code": result["code"], "reset": false,
			"windows_reset": result["windowsReset"], "message": msg,
		})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
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

	client := &http.Client{Timeout: 30 * time.Second}

	res, err := netutil.DoHTTP(client, req)
	if err != nil {
		return nil, err
	}

	defer res.Body.Close()

	var data map[string]any
	_ = json.NewDecoder(res.Body).Decode(&data)

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg := "Codex reset credits API unavailable"

		if data != nil {
			for _, k := range []string{"message", "error", "detail"} {
				if v, ok := data[k].(string); ok && v != "" {
					msg = v
					break
				}
			}
		}

		return nil, errString(msg)
	}

	creditsIn, _ := data["credits"].([]any)
	credits := make([]map[string]any, 0, len(creditsIn))

	for _, c := range creditsIn {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}

		status, _ := cm["status"].(string)
		if status == "" {
			status = "unknown"
		}

		credits = append(credits, map[string]any{
			"status":    status,
			"grantedAt": cm["granted_at"],
			"expiresAt": cm["expires_at"],
		})
	}

	avail := 0.0
	if v, ok := data["available_count"].(float64); ok {
		avail = v
	} else if v, ok := data["availableCount"].(float64); ok {
		avail = v
	}

	if avail < 0 {
		avail = 0
	}

	return map[string]any{"availableCount": int(avail), "credits": credits}, nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }
func errString(s string) error      { return simpleError(s) }

func consumeCodexRateLimitResetCredit(accessToken, redeemRequestID string) map[string]any {
	payload, _ := json.Marshal(map[string]string{"redeem_request_id": redeemRequestID})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, codexResetConsumeURL, bytes.NewReader(payload))
	if err != nil {
		return map[string]any{"ok": false, "message": err.Error(), "status": 500}
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}

	res, err := netutil.DoHTTP(client, req)
	if err != nil {
		return map[string]any{"ok": false, "message": err.Error(), "status": 500}
	}

	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	var data map[string]any

	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}

	code, _ := data["code"].(string)

	windowsReset := 0.0
	if v, ok := data["windows_reset"].(float64); ok {
		windowsReset = v
	}

	ok := res.StatusCode >= 200 && res.StatusCode < 300 && (code == "reset" || windowsReset > 0)
	noCredit := res.StatusCode >= 200 && res.StatusCode < 300 && code == "no_credit"
	msg, _ := data["message"].(string)

	var credit any
	if data != nil {
		credit = data["credit"]
	}

	return map[string]any{
		"ok": ok, "noCredit": noCredit, "status": res.StatusCode,
		"code": code, "windowsReset": windowsReset, "message": msg, "credit": credit,
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
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])

	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
