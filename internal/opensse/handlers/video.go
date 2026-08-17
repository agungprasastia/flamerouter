package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/config"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/model"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	defaultVideoProvider = "xai"
	defaultVideoBaseURL  = "https://api.x.ai/v1/videos"
)

// Video handles POST /v1/videos/{generations|edits|extensions}.
func Video(ctx context.Context, w http.ResponseWriter, r *http.Request, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	_ = exec

	action := videoActionFromPath(r.URL.Path)
	if action == "" {
		jsonError(w, http.StatusBadRequest, "Unknown video action")
		return nil
	}

	forwardBody, contentType, providerID, err := prepareVideoPayload(body, r.Header.Get("Content-Type"))
	if err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return err
	}

	conn, errMsg := pickVideoConn(st, fb, providerID, r.Header.Get("x-connection-id"))
	if errMsg != "" || conn == nil {
		if errMsg == "" {
			errMsg = "connection not found"
		}

		jsonError(w, http.StatusBadRequest, errMsg)

		return nil
	}

	return executeVideoRequest(ctx, w, r, conn, action, forwardBody, contentType, fb)
}

func prepareVideoPayload(body []byte, origCT string) ([]byte, string, string, error) {
	var parsed map[string]any

	contentType := origCT
	if strings.Contains(contentType, "application/json") || (len(body) > 0 && body[0] == '{') {
		if err := json.Unmarshal(body, &parsed); err != nil {
			return nil, "", "", fmt.Errorf("invalid json body")
		}
	}

	providerID, modelName, errMsg := resolveVideoProvider(parsed)
	if errMsg != "" {
		return nil, "", "", fmt.Errorf("%s", errMsg)
	}

	forwardBody := body

	if parsed != nil && modelName != "" {
		if m, _ := parsed["model"].(string); m != "" && m != modelName { //nolint:errcheck // optional type assertion
			parsed["model"] = modelName
			forwardBody, _ = json.Marshal(parsed) //nolint:errcheck // safe map marshal
			contentType = "application/json"
		}
	}

	return forwardBody, contentType, providerID, nil
}

func executeVideoRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, conn *store.Connection, action string, forwardBody []byte, contentType string, fb *fallback.Fallback) error {
	base := videoBaseURL(conn)
	upstreamURL := strings.TrimRight(base, "/") + "/" + action

	res, err := videoProxy(ctx, http.MethodPost, upstreamURL, forwardBody, contentType, r.Header.Get("Idempotency-Key"), mediaCredentials(conn))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	if res == nil || res.Body == nil {
		jsonError(w, http.StatusBadGateway, "nil response from upstream")
		return fmt.Errorf("nil response from upstream")
	}

	defer res.Body.Close()              //nolint:errcheck // best-effort body close
	respBody, _ := io.ReadAll(res.Body) //nolint:errcheck // best-effort read

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-9router-connection-id", conn.ID)
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(respBody) //nolint:errcheck // handler write

	return nil
}

// VideoPoll handles GET /v1/videos/{id} — poll async job status (xAI default).
func VideoPoll(ctx context.Context, w http.ResponseWriter, r *http.Request, requestID string, st *store.Store, fb *fallback.Fallback) error {
	if requestID == "" {
		jsonError(w, http.StatusBadRequest, "Missing video request id")
		return nil
	}

	providerID := defaultVideoProvider
	preferredID := r.Header.Get("x-connection-id")

	conn, errMsg := pickVideoConn(st, fb, providerID, preferredID)
	if errMsg != "" || conn == nil {
		if errMsg == "" {
			errMsg = "connection not found"
		}

		jsonError(w, http.StatusBadRequest, errMsg)

		return nil
	}

	base := videoBaseURL(conn)
	upstreamURL := strings.TrimRight(base, "/") + "/" + url.PathEscape(requestID)

	res, err := videoProxy(ctx, http.MethodGet, upstreamURL, nil, "", "", mediaCredentials(conn))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return err
	}

	if res == nil || res.Body == nil {
		jsonError(w, http.StatusBadGateway, "nil response from upstream")
		return fmt.Errorf("nil response from upstream")
	}

	defer res.Body.Close()              //nolint:errcheck // best-effort body close
	respBody, _ := io.ReadAll(res.Body) //nolint:errcheck // best-effort read

	if res.StatusCode < 400 {
		fb.ClearError(conn.ID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-9router-connection-id", conn.ID)
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(respBody) //nolint:errcheck // handler write

	return nil
}

func videoActionFromPath(path string) string {
	switch {
	case strings.Contains(path, "/extensions"):
		return "extensions"
	case strings.Contains(path, "/edits"):
		return "edits"
	case strings.Contains(path, "/generations"):
		return "generations"
	default:
		return ""
	}
}

func resolveVideoProvider(parsed map[string]any) (providerID, modelName, errMsg string) {
	if parsed == nil {
		return defaultVideoProvider, "", ""
	}

	modelStr, _ := parsed["model"].(string) //nolint:errcheck // optional type assertion
	if modelStr == "" {
		return defaultVideoProvider, "", ""
	}

	mref := model.ParseModel(modelStr)
	if mref.Provider == "" {
		// bare model id → default video provider
		if !strings.Contains(modelStr, "/") {
			return defaultVideoProvider, modelStr, ""
		}

		return "", "", "Combos are not supported for video generation"
	}

	providerID = model.ResolveProviderAlias(mref.Provider, nil)
	// only xai has videoConfig today; bare prefix-less already handled
	if providerID != "xai" && strings.Contains(modelStr, "/") {
		// allow if provider is xai alias; otherwise reject explicit non-video provider
		if providerID != defaultVideoProvider {
			return "", "", "Provider '" + providerID + "' does not support video generation"
		}
	}

	return providerID, mref.Model, ""
}

func pickVideoConn(st *store.Store, fb *fallback.Fallback, providerID, preferredID string) (*store.Connection, string) {
	if preferredID != "" {
		if c, _ := st.GetConnection(preferredID); c != nil { //nolint:errcheck // optional lookup
			return c, ""
		}
	}

	conn, _ := fb.SelectAccountExcluding(providerID, make(map[string]bool)) //nolint:errcheck // error handled via nil check
	if conn == nil {
		return nil, "No credentials for provider: " + providerID
	}

	return conn, ""
}

func videoBaseURL(conn *store.Connection) string {
	if conn != nil && conn.BaseURL != "" {
		b := strings.TrimRight(conn.BaseURL, "/")
		// chat base ends with /v1 or /v1/chat/completions → normalize to /v1/videos
		if strings.HasSuffix(b, "/videos") {
			return b
		}

		if i := strings.Index(b, "/v1"); i >= 0 {
			return b[:i+3] + "/videos"
		}

		return b + "/videos"
	}

	return defaultVideoBaseURL
}

func videoProxy(ctx context.Context, method, rawURL string, body []byte, contentType, idempotencyKey string, cred executor.Credentials) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, config.VideoFetchTimeout())
	defer cancel()

	var rdr io.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	if method == http.MethodPost {
		if contentType == "" {
			contentType = "application/json"
		}

		req.Header.Set("Content-Type", contentType)

		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
	}

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	return http.DefaultClient.Do(req)
}
