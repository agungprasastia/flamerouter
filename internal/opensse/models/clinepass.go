package models

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/shared/clineauth"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	clinepassModelsURL      = "https://api.cline.bot/api/v1/models"
	clinepassDefaultTimeout = 10 * time.Second
	clinepassCacheTTL       = 5 * time.Minute
)

// ClinePassResolver resolves dynamic models from https://api.cline.bot/api/v1/models.
type ClinePassResolver struct {
	Client *http.Client
}

func (r *ClinePassResolver) TTL() time.Duration {
	return clinepassCacheTTL
}

func (r *ClinePassResolver) client() *http.Client {
	if r.Client != nil {
		return r.Client
	}

	return http.DefaultClient
}

type clinepassRawModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (r *ClinePassResolver) fetchRaw(ctx context.Context, token string, isAPIKey bool) ([]clinepassRawModel, int, error) {
	ctx, cancel := context.WithTimeout(ctx, clinepassDefaultTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clinepassModelsURL, nil)
	if err != nil {
		return nil, 0, err
	}

	if isAPIKey {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	} else {
		headers := clineauth.BuildClineHeaders(token, map[string]string{"Accept": "application/json"})
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	resp, err := r.client().Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp == nil || resp.Body == nil {
		return nil, 0, fmt.Errorf("nil response from upstream")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, resp.StatusCode, fmt.Errorf("clinepass /models returned status %d: %s", resp.StatusCode, string(b))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	var list []clinepassRawModel
	if err := json.Unmarshal(bodyBytes, &list); err != nil {
		var wrapper struct {
			Data []clinepassRawModel `json:"data"`
		}

		if err2 := json.Unmarshal(bodyBytes, &wrapper); err2 != nil {
			return nil, resp.StatusCode, fmt.Errorf("decode clinepass models: %w", err)
		}

		list = wrapper.Data
	}

	return list, resp.StatusCode, nil
}

func (r *ClinePassResolver) Resolve(ctx context.Context, conn *store.Connection) ([]DynamicModel, error) {
	isAPIKey := conn.APIKey != ""

	token := conn.APIKey
	if token == "" {
		token = conn.AccessToken
	}

	if token == "" {
		return nil, nil
	}

	raw, _, err := r.fetchRaw(ctx, token, isAPIKey)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)

	var out []DynamicModel

	for _, m := range raw {
		id := strings.TrimSpace(m.ID)
		if id == "" || !strings.HasPrefix(id, "cline-pass/") || seen[id] {
			continue
		}

		seen[id] = true

		name := strings.TrimSpace(m.Name)
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
