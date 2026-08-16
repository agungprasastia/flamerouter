package models

import (
	"context"
	"encoding/json"
	"flamerouter/internal/store"
	"flamerouter/internal/tokenrefresh"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	copilotModelsURL      = "https://api.githubcopilot.com/models"
	copilotVSCodeVersion  = "1.98.2"
	copilotChatVersion    = "0.25.1"
	copilotUserAgent      = "GitHubCopilotChat/" + copilotChatVersion
	copilotAPIVersion     = "2025-04-01"
	copilotDefaultTimeout = 10 * time.Second
	copilotCacheTTL       = 5 * time.Minute
)

// CopilotResolver resolves dynamic models from GitHub Copilot's /models endpoint.
type CopilotResolver struct {
	Client         *http.Client
	RefreshManager *tokenrefresh.RefreshManager
}

func (r *CopilotResolver) TTL() time.Duration {
	return copilotCacheTTL
}

func (r *CopilotResolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}

	return http.DefaultClient
}

func (r *CopilotResolver) buildHeaders(token string) http.Header {
	h := make(http.Header)
	h.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("Copilot-Integration-Id", "vscode-chat")
	h.Set("editor-version", fmt.Sprintf("vscode/%s", copilotVSCodeVersion))
	h.Set("editor-plugin-version", fmt.Sprintf("copilot-chat/%s", copilotChatVersion))
	h.Set("user-agent", copilotUserAgent)
	h.Set("x-github-api-version", copilotAPIVersion)

	return h
}

type copilotRawModel struct {
	Capabilities *struct {
		Type string `json:"type"`
	} `json:"capabilities"`
	Policy *struct {
		State string `json:"state"`
	} `json:"policy"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type copilotResponse struct {
	Data []copilotRawModel `json:"data"`
}

func (r *CopilotResolver) fetchRaw(ctx context.Context, token string) ([]copilotRawModel, int, error) {
	ctx, cancel := context.WithTimeout(ctx, copilotDefaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, copilotModelsURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header = r.buildHeaders(token)

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp == nil || resp.Body == nil {
		return nil, 0, fmt.Errorf("nil response from upstream")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("copilot /models returned status %d: %s", resp.StatusCode, string(body))
	}

	var parsed copilotResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode copilot models response: %w", err)
	}

	return parsed.Data, resp.StatusCode, nil
}

func (r *CopilotResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	token := conn.AccessToken
	if token == "" && conn.ProviderSpecificData != nil {
		if ct, ok := conn.ProviderSpecificData["copilotToken"].(string); ok && ct != "" {
			token = ct
		}
	}

	if token == "" {
		token = conn.APIKey
	}

	if token == "" {
		return nil, nil
	}

	raw, statusCode, err := r.fetchRaw(ctx, token)
	if err != nil && (statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden) && conn.RefreshToken != "" {
		rm := r.RefreshManager
		if rm == nil {
			rm = tokenrefresh.NewRefreshManager()
		}

		refreshed, refErr := rm.Refresh(ctx, "github", conn.RefreshToken)
		if refErr == nil && refreshed != nil && refreshed.AccessToken != "" {
			token = refreshed.AccessToken
			raw, _, err = r.fetchRaw(ctx, token)
		}
	}

	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)

	var out []DynamicModel

	for _, m := range raw {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}

		if m.Capabilities != nil && m.Capabilities.Type != "chat" {
			continue
		}

		if m.Policy != nil && m.Policy.State != "" && m.Policy.State != "enabled" {
			continue
		}

		seen[id] = true

		name := m.Name
		if name == "" {
			name = id
		}

		out = append(out, DynamicModel{
			ID:   id,
			Name: name,
		})
	}

	return out, nil
}
