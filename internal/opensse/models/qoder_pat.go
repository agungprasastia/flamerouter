package models

import (
	"bytes"
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/shared/qoder"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	patRefreshBuffer = 5 * time.Minute
	patDefaultTTL    = 24 * time.Hour
)

type patCacheItem struct {
	expiresAt   time.Time
	accessToken string
	userID      string
}

var (
	patMu    sync.RWMutex
	patCache = make(map[string]patCacheItem)
)

type jobTokenExchangeResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    string `json:"expires_at"`
	ExpiresIn    int64  `json:"expires_in"`
}

func parseJobTokenExpiry(data jobTokenExchangeResponse) time.Time {
	expiresAt := time.Now().Add(patDefaultTTL)

	if data.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, data.ExpiresAt); err == nil {
			return t
		}
	}

	if data.ExpiresIn > 0 {
		return time.Now().Add(time.Duration(data.ExpiresIn) * time.Second)
	}

	return expiresAt
}

func exchangeJobToken(ctx context.Context, client *http.Client, pat string) (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	payload, err := json.Marshal(map[string]string{"personal_token": pat})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal pat payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qoder.JobTokenExchangeURL, bytes.NewReader(payload))
	if err != nil {
		return "", time.Time{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "qodercli/1.0.0")
	req.Header.Set("Cosy-Version", qoder.IDEVersion)
	req.Header.Set("Cosy-ClientType", qoder.ClientType)

	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}

	if resp == nil || resp.Body == nil {
		return "", time.Time{}, fmt.Errorf("nil response from upstream")
	}

	defer func() {
		//nolint:errcheck // best effort close
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", time.Time{}, fmt.Errorf("read qoder pat error response: %w", err)
		}

		return "", time.Time{}, fmt.Errorf("qoder PAT exchange failed with status %d: %s", resp.StatusCode, string(b))
	}

	var data jobTokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", time.Time{}, err
	}

	if data.Token == "" {
		return "", time.Time{}, fmt.Errorf("qoder PAT exchange returned empty job token")
	}

	return data.Token, parseJobTokenExpiry(data), nil
}

func fetchUserIDForJobToken(ctx context.Context, client *http.Client, jobToken string) string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qoder.UserInfoURL, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", jobToken))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "qodercli/1.0.0")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}

	if resp == nil || resp.Body == nil {
		return ""
	}

	defer func() {
		//nolint:errcheck // best effort close
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return ""
	}

	for _, k := range []string{"id", "userId", "user_id"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}

	return ""
}

// ResolvePatCredential resolves and caches a Qoder PAT to its corresponding job token and user id.
func ResolvePatCredential(ctx context.Context, client *http.Client, pat string) (string, string, error) {
	patMu.RLock()
	cached, ok := patCache[pat]
	patMu.RUnlock()

	if ok && time.Until(cached.expiresAt) > patRefreshBuffer {
		return cached.accessToken, cached.userID, nil
	}

	if client == nil {
		client = http.DefaultClient
	}

	jobToken, expiresAt, err := exchangeJobToken(ctx, client, pat)
	if err != nil {
		return "", "", err
	}

	userID := fetchUserIDForJobToken(ctx, client, jobToken)

	patMu.Lock()
	patCache[pat] = patCacheItem{
		accessToken: jobToken,
		userID:      userID,
		expiresAt:   expiresAt,
	}
	patMu.Unlock()

	return jobToken, userID, nil
}
