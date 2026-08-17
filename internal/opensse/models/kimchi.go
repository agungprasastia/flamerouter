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

// TTL returns cache TTL for Kimchi models.
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

	if resp == nil || resp.Body == nil {
		return nil, 0, fmt.Errorf("nil response from upstream")
	}

	defer func() {
		//nolint:errcheck // best effort close
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, resp.StatusCode, fmt.Errorf("read kimchi error response: %w", err)
		}

		return nil, resp.StatusCode, fmt.Errorf("kimchi metadata returned status %d: %s", resp.StatusCode, string(b))
	}

	var parsed kimchiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode kimchi models response: %w", err)
	}

	return parsed.Models, resp.StatusCode, nil
}

func extractKimchiTokenAndEndpoint(conn *store.Connection) (string, string) {
	token := conn.AccessToken
	if token == "" {
		token = conn.APIKey
	}

	if token == "" && conn.ProviderSpecificData != nil {
		if k, ok := conn.ProviderSpecificData["apiKey"].(string); ok && k != "" {
			token = k
		}
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

	return token, endpoint
}

func extractKimchiIDAndName(item kimchiRawItem) (string, string) {
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

	name := strings.TrimSpace(item.DisplayName)
	if name == "" {
		name = strings.TrimSpace(item.Name)
	}

	if name == "" {
		name = id
	}

	return id, name
}

func parseKimchiItem(item kimchiRawItem) DynamicModel {
	id, name := extractKimchiIDAndName(item)
	ctxLen, maxOut := item.ContextLength, item.MaxOutputTokens

	if item.Limits != nil {
		if item.Limits.ContextWindow > 0 {
			ctxLen = item.Limits.ContextWindow
		}

		if item.Limits.MaxOutputTokens > 0 {
			maxOut = item.Limits.MaxOutputTokens
		}
	}

	isVL := false

	for _, m := range item.InputModalities {
		if m == "image" {
			isVL = true
			break
		}
	}

	caps := map[string]any{"vision": isVL, "reasoning": item.Reasoning}
	if ctxLen > 0 {
		caps["contextWindow"] = ctxLen
	}

	if maxOut > 0 {
		caps["maxOutput"] = maxOut
	}

	if item.Provider != "" {
		caps["upstreamProvider"] = item.Provider
	}

	return DynamicModel{
		ID:              id,
		Name:            name,
		ContextLength:   ctxLen,
		MaxOutputTokens: maxOut,
		IsReasoning:     item.Reasoning,
		IsVL:            isVL,
		Capabilities:    caps,
		RawConfig:       nil,
		UpstreamModelID: "",
		Description:     "",
		RateMultiplier:  0,
	}
}

// Resolve retrieves active dynamic models from Kimchi API.
func (r *KimchiResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	token, endpoint := extractKimchiTokenAndEndpoint(conn)
	if token == "" {
		return nil, nil
	}

	raw, _, err := r.fetchRaw(ctx, token, endpoint)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	out := make([]DynamicModel, 0, len(raw))

	for _, item := range raw {
		model := parseKimchiItem(item)
		if model.ID == "" || seen[model.ID] {
			continue
		}

		seen[model.ID] = true

		out = append(out, model)
	}

	return out, nil
}
