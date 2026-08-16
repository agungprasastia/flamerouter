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

func parseGrokCliRawJSON(data []byte) ([]DynamicModel, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

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
		if dataArr, ok := v["data"].([]any); ok {
			for _, item := range dataArr {
				if m, ok := item.(map[string]any); ok {
					items = append(items, m)
				}
			}
		} else if modelsArr, ok := v["models"].([]any); ok {
			for _, item := range modelsArr {
				if m, ok := item.(map[string]any); ok {
					items = append(items, m)
				}
			}
		} else if resultsArr, ok := v["results"].([]any); ok {
			for _, item := range resultsArr {
				if m, ok := item.(map[string]any); ok {
					items = append(items, m)
				}
			}
		} else {
			for k, val := range v {
				if m, ok := val.(map[string]any); ok {
					if _, hasID := m["id"]; !hasID {
						m["id"] = k
					}

					items = append(items, m)
				} else if s, ok := val.(string); ok {
					items = append(items, map[string]any{"id": s, "name": k})
				}
			}
		}
	}

	seen := make(map[string]bool)

	var out []DynamicModel

	for _, item := range items {
		id := ""

		for _, k := range []string{"id", "model_id", "modelId", "model", "slug", "name"} {
			if s, ok := item[k].(string); ok && strings.TrimSpace(s) != "" {
				id = strings.TrimSpace(s)
				break
			}
		}

		if id == "" || seen[id] {
			continue
		}

		seen[id] = true

		name := id

		for _, k := range []string{"display_name", "displayName", "name"} {
			if s, ok := item[k].(string); ok && strings.TrimSpace(s) != "" {
				name = strings.TrimSpace(s)
				break
			}
		}

		ctxLen := 0

		for _, k := range []string{"context_length", "contextLength", "context_window", "contextWindow"} {
			if num, ok := item[k].(float64); ok && num > 0 {
				ctxLen = int(num)
				break
			} else if s, ok := item[k].(string); ok {
				if n, err := strconv.Atoi(s); err == nil && n > 0 {
					ctxLen = n
					break
				}
			}
		}

		maxOut := 0

		for _, k := range []string{"max_output_tokens", "maxOutputTokens"} {
			if num, ok := item[k].(float64); ok && num > 0 {
				maxOut = int(num)
				break
			} else if s, ok := item[k].(string); ok {
				if n, err := strconv.Atoi(s); err == nil && n > 0 {
					maxOut = n
					break
				}
			}
		}

		if id == grokCliModelDefault {
			if ctxLen == 0 {
				ctxLen = 500000
			}

			if maxOut == 0 {
				maxOut = 64000
			}
		}

		out = append(out, DynamicModel{
			ID:              id,
			Name:            name,
			ContextLength:   ctxLen,
			MaxOutputTokens: maxOut,
		})
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
	defer resp.Body.Close()

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

func (r *GrokCliResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	if conn.AccessToken == "" && conn.APIKey == "" {
		return nil, nil
	}

	models, statusCode, err := r.fetchRaw(ctx, conn)
	if err != nil && (statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden) && conn.RefreshToken != "" {
		rm := r.RefreshManager
		if rm == nil {
			rm = tokenrefresh.NewRefreshManager()
		}

		refreshed, refErr := rm.Refresh(ctx, "grok-cli", conn.RefreshToken)
		if refErr == nil && refreshed != nil && refreshed.AccessToken != "" {
			connCopy := *conn
			connCopy.AccessToken = refreshed.AccessToken

			if refreshed.RefreshToken != "" {
				connCopy.RefreshToken = refreshed.RefreshToken
			}

			models, _, err = r.fetchRaw(ctx, &connCopy)
		}
	}

	if err != nil {
		return nil, err
	}

	return models, nil
}
