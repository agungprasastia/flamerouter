package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"flamerouter/internal/opensse/shared/qoder"
)

const (
	patRefreshBuffer = 5 * time.Minute
	patDefaultTTL    = 24 * time.Hour
)

type patCacheItem struct {
	accessToken string
	userId      string
	expiresAt   time.Time
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

func exchangeJobToken(ctx context.Context, client *http.Client, pat string) (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	payload, _ := json.Marshal(map[string]string{"personal_token": pat})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, qoder.QODER_JOB_TOKEN_EXCHANGE_URL, bytes.NewReader(payload))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "qodercli/1.0.0")
	req.Header.Set("Cosy-Version", qoder.QODER_IDE_VERSION)
	req.Header.Set("Cosy-ClientType", qoder.QODER_CLIENT_TYPE)

	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("qoder PAT exchange failed with status %d: %s", resp.StatusCode, string(b))
	}

	var data jobTokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", time.Time{}, err
	}
	if data.Token == "" {
		return "", time.Time{}, fmt.Errorf("qoder PAT exchange returned empty job token")
	}

	expiresAt := time.Now().Add(patDefaultTTL)
	if data.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, data.ExpiresAt); err == nil {
			expiresAt = t
		}
	} else if data.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(data.ExpiresIn) * time.Second)
	}

	return data.Token, expiresAt, nil
}

func fetchUserIDForJobToken(ctx context.Context, client *http.Client, jobToken string) string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qoder.QODER_USERINFO_URL, nil)
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
	defer resp.Body.Close()

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
		return cached.accessToken, cached.userId, nil
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
		userId:      userID,
		expiresAt:   expiresAt,
	}
	patMu.Unlock()

	return jobToken, userID, nil
}
