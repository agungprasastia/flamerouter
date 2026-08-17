package models

import (
	"context"
	"encoding/json"
	"flamerouter/internal/store"
	"flamerouter/internal/tokenrefresh"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	grokCliModelsURL      = "https://cli-chat-proxy.grok.com/v1/models"
	grokCliVersion        = "0.2.99"
	grokCliUserAgent      = "grok-shell/0.2.99 (linux; x86_64)"
	grokCliClientID       = "grok-shell"
	grokCliModelDefault   = "grok-build"
	grokCliDefaultTimeout = 15 * time.Second
	grokCliCacheTTL       = 5 * time.Minute
)

// GrokCliResolver resolves dynamic models from cli-chat-proxy.grok.com/v1/models.
type GrokCliResolver struct {
	Client         *http.Client
	RefreshManager *tokenrefresh.RefreshManager
}

// TTL returns cache TTL for GrokCli models.
func (r *GrokCliResolver) TTL() time.Duration {
	return grokCliCacheTTL
}

func (r *GrokCliResolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}

	return http.DefaultClient
}

func (r *GrokCliResolver) buildHeaders(conn *store.Connection) http.Header {
	h := make(http.Header)

	tok := conn.AccessToken
	if tok == "" {
		tok = conn.APIKey
	}

	h.Set("Authorization", fmt.Sprintf("Bearer %s", tok))
	h.Set("Accept", "application/json")
	h.Set("User-Agent", grokCliUserAgent)
	h.Set("x-xai-token-auth", "xai-grok-cli")
	h.Set("x-grok-client-version", grokCliVersion)
	h.Set("x-grok-client-identifier", grokCliClientID)
	h.Set("x-grok-client-mode", "headless")

	if conn.ProviderSpecificData != nil {
		if email, ok := conn.ProviderSpecificData["email"].(string); ok && email != "" {
			h.Set("x-email", email)
		}

		if uid, ok := conn.ProviderSpecificData["userId"].(string); ok && uid != "" {
			h.Set("x-userid", uid)
		} else if pid, ok := conn.ProviderSpecificData["principalId"].(string); ok && pid != "" {
			h.Set("x-userid", pid)
		}
	}

	return h
}

func extractMapItems(v map[string]any) []map[string]any {
	for _, key := range []string{"data", "models", "results"} {
		if arr, ok := v[key].([]any); ok {
			var res []map[string]any

			for _, item := range arr {
				if m, ok := item.(map[string]any); ok {
					res = append(res, m)
				}
			}

			return res
		}
	}

	var res []map[string]any

	for k, val := range v {
		if m, ok := val.(map[string]any); ok {
			if _, hasID := m["id"]; !hasID {
				m["id"] = k
			}

			res = append(res, m)
		} else if s, ok := val.(string); ok {
			res = append(res, map[string]any{"id": s, "name": k})
		}
	}

	return res
}

func extractGrokItems(raw any) []map[string]any {
	var items []map[string]any

	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			} else if s, ok := item.(string); ok {
				items = append(items, map[string]any{"id": s})
			}
		}
	case map[string]any:
		items = extractMapItems(v)
	}

	return items
}

func extractGrokIntField(item map[string]any, keys []string) int {
	for _, k := range keys {
		if num, ok := item[k].(float64); ok && num > 0 {
			return int(num)
		}

		if s, ok := item[k].(string); ok {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				return n
			}
		}
	}

	return 0
}

func extractGrokStringField(item map[string]any, keys []string) string {
	for _, k := range keys {
		if s, ok := item[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}

	return ""
}

func parseGrokModelItem(item map[string]any) DynamicModel {
	id := extractGrokStringField(item, []string{"id", "model_id", "modelId", "model", "slug", "name"})
	if id == "" {
		return DynamicModel{
			ID:              "",
			Name:            "",
			Capabilities:    nil,
			RawConfig:       nil,
			UpstreamModelID: "",
			Description:     "",
			ContextLength:   0,
			MaxOutputTokens: 0,
			RateMultiplier:  0,
			IsReasoning:     false,
			IsVL:            false,
		}
	}

	name := extractGrokStringField(item, []string{"display_name", "displayName", "name"})
	if name == "" {
		name = id
	}

	ctxLen := extractGrokIntField(item, []string{"context_length", "contextLength", "context_window", "contextWindow"})
	maxOut := extractGrokIntField(item, []string{"max_output_tokens", "maxOutputTokens"})

	if id == grokCliModelDefault {
		if ctxLen == 0 {
			ctxLen = 500000
		}

		if maxOut == 0 {
			maxOut = 64000
		}
	}

	return DynamicModel{
		ID:              id,
		Name:            name,
		ContextLength:   ctxLen,
		MaxOutputTokens: maxOut,
		IsReasoning:     false,
		IsVL:            false,
		Capabilities:    nil,
		RawConfig:       nil,
		UpstreamModelID: "",
		Description:     "",
		RateMultiplier:  0,
	}
}

func parseGrokCliRawJSON(data []byte) ([]DynamicModel, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	items := extractGrokItems(raw)
	seen := make(map[string]bool)
	out := make([]DynamicModel, 0, len(items))

	for _, item := range items {
		model := parseGrokModelItem(item)
		if model.ID == "" || seen[model.ID] {
			continue
		}

		seen[model.ID] = true

		out = append(out, model)
	}

	return out, nil
}

func (r *GrokCliResolver) fetchRaw(ctx context.Context, conn *store.Connection) ([]DynamicModel, int, error) {
	ctx, cancel := context.WithTimeout(ctx, grokCliDefaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, grokCliModelsURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header = r.buildHeaders(conn)

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, 0, err
	}

	if resp == nil || resp.Body == nil {
		return nil, 0, fmt.Errorf("nil response from upstream")
	}

	defer func() {
		//nolint:errcheck // best effort close
		_ = resp.Body.Close()
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("grok-cli /models returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	models, err := parseGrokCliRawJSON(bodyBytes)

	return models, resp.StatusCode, err
}

func (r *GrokCliResolver) tryRefresh(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	if conn == nil {
		return nil, fmt.Errorf("nil connection")
	}

	rm := r.RefreshManager
	if rm == nil {
		rm = tokenrefresh.NewRefreshManager()
	}

	authResult, authErr := rm.Refresh(ctx, "grok-cli", conn.RefreshToken)
	if authErr != nil || authResult == nil || authResult.AccessToken == "" {
		return nil, authErr
	}

	connCopy := *conn
	connCopy.AccessToken = authResult.AccessToken

	if authResult.RefreshToken != "" {
		connCopy.RefreshToken = authResult.RefreshToken
	}

	m, _, e := r.fetchRaw(ctx, &connCopy)
	if e != nil {
		return nil, e
	}

	return m, nil
}

// Resolve retrieves dynamic models for Grok CLI connection.
func (r *GrokCliResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	if conn.AccessToken == "" && conn.APIKey == "" {
		return nil, nil
	}

	models, statusCode, err := r.fetchRaw(ctx, conn)
	if err != nil && (statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden) && conn.RefreshToken != "" {
		refModels, refErr := r.tryRefresh(ctx, conn)
		if refErr == nil {
			return refModels, nil
		}
	}

	if err != nil {
		return nil, err
	}

	return models, nil
}
