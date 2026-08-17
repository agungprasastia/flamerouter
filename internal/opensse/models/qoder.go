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

// TTL returns cache TTL for Qoder models.
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
	req, cancel, err := buildQoderModelRequest(ctx, creds)
	if err != nil {
		return nil, 0, err
	}
	defer cancel()

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
		rawBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, resp.StatusCode, fmt.Errorf("read qoder error response: %w", readErr)
		}

		return nil, resp.StatusCode, fmt.Errorf("qoder model/list returned status %d: %s", resp.StatusCode, string(rawBytes))
	}

	var chatResp qoderModelListResponse
	if decodeErr := json.NewDecoder(resp.Body).Decode(&chatResp); decodeErr != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode qoder models response: %w", decodeErr)
	}

	return chatResp.Chat, resp.StatusCode, nil
}

func buildQoderModelRequest(ctx context.Context, creds qoder.CosyCreds) (*http.Request, context.CancelFunc, error) {
	modelListURL := qoder.ModelListURL
	if strings.HasPrefix(creds.AuthToken, "jt-") {
		modelListURL = fmt.Sprintf("%s/algo/api/v2/model/list", qoder.ChatBaseAlt)
	}

	headers, err := qoder.BuildCosyHeaders(nil, modelListURL, creds)
	if err != nil {
		return nil, nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, qoderDefaultTimeout)

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, modelListURL, nil)
	if err != nil {
		cancel()
		return nil, nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "identity")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return req, cancel, nil
}

func extractQoderAuth(conn *store.Connection) (string, string) {
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

	return authToken, userID
}

func extractQoderMetadata(conn *store.Connection) (string, string) {
	machineID, email := "", ""

	if conn.ProviderSpecificData != nil {
		if mid, ok := conn.ProviderSpecificData["machineId"].(string); ok {
			machineID = mid
		}

		if em, ok := conn.ProviderSpecificData["email"].(string); ok {
			email = em
		}
	}

	return machineID, email
}

func (r *QoderResolver) resolveAuthCreds(ctx context.Context, conn *store.Connection) (qoder.CosyCreds, error) {
	authToken, userID := extractQoderAuth(conn)

	if strings.HasPrefix(authToken, "pt-") {
		jt, uid, err := ResolvePatCredential(ctx, r.client(), authToken)
		if err != nil {
			return qoder.CosyCreds{
				UserID:    "",
				AuthToken: "",
				Name:      "",
				Email:     "",
				MachineID: "",
			}, err
		}

		authToken = jt

		if uid != "" {
			userID = uid
		}
	}

	if authToken == "" || userID == "" {
		return qoder.CosyCreds{
			UserID:    "",
			AuthToken: "",
			Name:      "",
			Email:     "",
			MachineID: "",
		}, nil
	}

	machineID, email := extractQoderMetadata(conn)

	return qoder.CosyCreds{
		UserID:    userID,
		AuthToken: authToken,
		Name:      conn.Name,
		Email:     email,
		MachineID: machineID,
	}, nil
}

func filterQoderModels(raw []qoderChatEntry) []DynamicModel {
	seen := make(map[string]bool)
	out := make([]DynamicModel, 0, len(raw))

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
			Capabilities:    nil,
			RawConfig:       nil,
			UpstreamModelID: "",
			RateMultiplier:  0,
		})
	}

	return out
}

// Resolve retrieves dynamic models for Qoder connection.
func (r *QoderResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	creds, err := r.resolveAuthCreds(ctx, conn)
	if err != nil {
		return nil, err
	}

	if creds.AuthToken == "" || creds.UserID == "" {
		return nil, nil
	}

	raw, _, err := r.fetchRaw(ctx, creds)
	if err != nil {
		return nil, err
	}

	return filterQoderModels(raw), nil
}
