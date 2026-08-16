package models

import (
	"context"
	"encoding/json"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	kimchiDefaultAPI     = "https://llm.kimchi.dev"
	kimchiUserAgent      = "kimchi/0.1.40"
	kimchiDefaultTimeout = 20 * time.Second
	kimchiCacheTTL       = 5 * time.Minute
)

// KimchiResolver resolves dynamic models from https://llm.kimchi.dev/v1/models/metadata?include_in_cli=true.
type KimchiResolver struct {
	Client *http.Client
}

func (r *KimchiResolver) TTL() time.Duration {
	return kimchiCacheTTL
}

func (r *KimchiResolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}

	return http.DefaultClient
}

func normalizeKimchiEndpoint(ep string) string {
	raw := strings.TrimSpace(ep)
	if raw == "" {
		return kimchiDefaultAPI
	}

	return strings.TrimRight(raw, "/")
}

type kimchiRawItem struct {
	Limits *struct {
		ContextWindow   int `json:"context_window"`
		MaxOutputTokens int `json:"max_output_tokens"`
	} `json:"limits"`
	ID              string   `json:"id"`
	Slug            string   `json:"slug"`
	Model           string   `json:"model"`
	Name            string   `json:"name"`
	DisplayName     string   `json:"display_name"`
	Provider        string   `json:"provider"`
	InputModalities []string `json:"input_modalities"`
	ContextLength   int      `json:"contextLength"`
	MaxOutputTokens int      `json:"maxOutputTokens"`
	Reasoning       bool     `json:"reasoning"`
}

type kimchiResponse struct {
	Models []kimchiRawItem `json:"models"`
}

func (r *KimchiResolver) fetchRaw(ctx context.Context, token, endpoint string) ([]kimchiRawItem, int, error) {
	reqURL := fmt.Sprintf("%s/v1/models/metadata?include_in_cli=true", normalizeKimchiEndpoint(endpoint))

	ctx, cancel := context.WithTimeout(ctx, kimchiDefaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("User-Agent", kimchiUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("kimchi metadata returned status %d: %s", resp.StatusCode, string(b))
	}

	var parsed kimchiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode kimchi models response: %w", err)
	}

	return parsed.Models, resp.StatusCode, nil
}

func (r *KimchiResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	token := conn.AccessToken
	if token == "" {
		token = conn.APIKey
	}

	if token == "" && conn.ProviderSpecificData != nil {
		if k, ok := conn.ProviderSpecificData["apiKey"].(string); ok && k != "" {
			token = k
		}
	}

	if token == "" {
		return nil, nil
	}

	endpoint := kimchiDefaultAPI

	if conn.ProviderSpecificData != nil {
		if ep, ok := conn.ProviderSpecificData["kimchiEndpoint"].(string); ok && ep != "" {
			endpoint = ep
		}
	}

	if conn.BaseURL != "" {
		endpoint = conn.BaseURL
	}

	raw, _, err := r.fetchRaw(ctx, token, endpoint)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)

	var out []DynamicModel

	for _, item := range raw {
		id := strings.TrimSpace(item.Slug)
		if id == "" {
			id = strings.TrimSpace(item.ID)
		}

		if id == "" {
			id = strings.TrimSpace(item.Model)
		}

		if id == "" {
			id = strings.TrimSpace(item.Name)
		}

		if id == "" || seen[id] {
			continue
		}

		seen[id] = true

		name := strings.TrimSpace(item.DisplayName)
		if name == "" {
			name = strings.TrimSpace(item.Name)
		}

		if name == "" {
			name = id
		}

		ctxLen := 0
		maxOut := 0

		if item.Limits != nil {
			ctxLen = item.Limits.ContextWindow
			maxOut = item.Limits.MaxOutputTokens
		}

		if ctxLen == 0 {
			ctxLen = item.ContextLength
		}

		if maxOut == 0 {
			maxOut = item.MaxOutputTokens
		}

		isVL := false

		for _, m := range item.InputModalities {
			if m == "image" {
				isVL = true
				break
			}
		}

		caps := map[string]any{
			"vision":    isVL,
			"reasoning": item.Reasoning,
		}
		if ctxLen > 0 {
			caps["contextWindow"] = ctxLen
		}

		if maxOut > 0 {
			caps["maxOutput"] = maxOut
		}

		if item.Provider != "" {
			caps["upstreamProvider"] = item.Provider
		}

		out = append(out, DynamicModel{
			ID:              id,
			Name:            name,
			ContextLength:   ctxLen,
			MaxOutputTokens: maxOut,
			IsReasoning:     item.Reasoning,
			IsVL:            isVL,
			Capabilities:    caps,
		})
	}

	return out, nil
}
