package models

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/shared/qoder"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	qoderDefaultTimeout = 15 * time.Second
	qoderCacheTTL       = 60 * time.Minute
)

// QoderResolver resolves dynamic models from Qoder's COSY-signed /algo/api/v2/model/list endpoint.
type QoderResolver struct {
	Client *http.Client
}

func (r *QoderResolver) TTL() time.Duration {
	return qoderCacheTTL
}

func (r *QoderResolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}

	return http.DefaultClient
}

type qoderChatEntry struct {
	Enable          *bool  `json:"enable"`
	Key             string `json:"key"`
	DisplayName     string `json:"display_name"`
	Description     string `json:"description"`
	MaxInputTokens  int    `json:"max_input_tokens"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	IsVL            bool   `json:"is_vl"`
	IsReasoning     bool   `json:"is_reasoning"`
}

type qoderModelListResponse struct {
	Chat []qoderChatEntry `json:"chat"`
}

func (r *QoderResolver) fetchRaw(ctx context.Context, creds qoder.CosyCreds) ([]qoderChatEntry, int, error) {
	modelListURL := qoder.QODER_MODEL_LIST_URL
	if strings.HasPrefix(creds.AuthToken, "jt-") {
		modelListURL = fmt.Sprintf("%s/algo/api/v2/model/list", qoder.QODER_CHAT_BASE_ALT)
	}

	headers, err := qoder.BuildCosyHeaders(nil, modelListURL, creds)
	if err != nil {
		return nil, 0, err
	}

	ctx, cancel := context.WithTimeout(ctx, qoderDefaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelListURL, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("qoder model/list returned status %d: %s", resp.StatusCode, string(b))
	}

	var parsed qoderModelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode qoder models response: %w", err)
	}

	return parsed.Chat, resp.StatusCode, nil
}

func (r *QoderResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	authToken := conn.AccessToken
	if authToken == "" {
		authToken = conn.APIKey
	}

	userID := ""

	if conn.ProviderSpecificData != nil {
		if u, ok := conn.ProviderSpecificData["userId"].(string); ok {
			userID = u
		}
	}

	if strings.HasPrefix(authToken, "pt-") {
		jt, uid, err := ResolvePatCredential(ctx, r.client(), authToken)
		if err != nil {
			return nil, err
		}

		authToken = jt

		if uid != "" {
			userID = uid
		}
	}

	if authToken == "" || userID == "" {
		return nil, nil
	}

	machineID := ""
	email := ""

	if conn.ProviderSpecificData != nil {
		if mid, ok := conn.ProviderSpecificData["machineId"].(string); ok {
			machineID = mid
		}

		if em, ok := conn.ProviderSpecificData["email"].(string); ok {
			email = em
		}
	}

	creds := qoder.CosyCreds{
		UserID:    userID,
		AuthToken: authToken,
		Name:      conn.Name,
		Email:     email,
		MachineID: machineID,
	}

	raw, _, err := r.fetchRaw(ctx, creds)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)

	var out []DynamicModel

	for _, entry := range raw {
		key := strings.TrimSpace(entry.Key)
		if key == "" || seen[key] {
			continue
		}

		seen[key] = true

		if entry.Enable != nil && !*entry.Enable {
			continue
		}

		display := strings.TrimSpace(entry.DisplayName)
		if display == "" {
			display = key
		}

		ctxLen := entry.MaxInputTokens
		if ctxLen <= 0 {
			ctxLen = 131072
		}

		out = append(out, DynamicModel{
			ID:              key,
			Name:            display,
			ContextLength:   ctxLen,
			MaxOutputTokens: entry.MaxOutputTokens,
			IsReasoning:     entry.IsReasoning,
			IsVL:            entry.IsVL,
			Description:     entry.Description,
		})
	}

	return out, nil
}
