package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ExchangeGithubDeviceToken polls GitHub device flow then optionally fetches Copilot token.
func ExchangeGithubDeviceToken(ctx context.Context, deviceCode string, wantCopilot bool) (*Token, map[string]any, error) {
	cfg := ProviderConfigs["github"]
	tok, err := PollDeviceToken(ctx, cfg, deviceCode, 5)
	if err != nil {
		return nil, nil, err
	}
	extra := map[string]any{}
	if wantCopilot {
		ct, exp, err := fetchCopilotToken(ctx, tok.AccessToken)
		if err == nil && ct != "" {
			extra["copilotToken"] = ct
			extra["copilotTokenExpiresAt"] = exp
			// Prefer copilot token as access for github executor
			tok.AccessToken = ct
		}
	}
	return tok, extra, nil
}

func fetchCopilotToken(ctx context.Context, githubToken string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CopilotTokenURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "token "+githubToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/1.98.2")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.25.1")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.25.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("copilot token: %s", string(body))
	}
	var out struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", 0, err
	}
	return out.Token, out.ExpiresAt, nil
}

// RefreshKiroToken refreshes Kiro social/desktop tokens.
func RefreshKiroToken(ctx context.Context, refreshToken string, psd map[string]any) (*Token, error) {
	// Social auth refresh endpoint
	authMethod := ""
	if psd != nil {
		if m, ok := psd["authMethod"].(string); ok {
			authMethod = m
		}
	}
	if authMethod == "api_key" {
		return nil, fmt.Errorf("api_key auth does not refresh")
	}

	// Default: social refresh
	urlStr := "https://prod.us-east-1.auth.desktop.kiro.dev/refreshToken"
	if cfg := ProviderConfigs["kiro"]; cfg.RefreshURL != "" {
		urlStr = cfg.RefreshURL
	}
	payload := map[string]any{"refreshToken": refreshToken}
	if psd != nil {
		if clientId, ok := psd["clientId"].(string); ok {
			payload["clientId"] = clientId
		}
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		// try AWS SSO OIDC form for idc
		return refreshKiroOIDC(ctx, refreshToken, psd)
	}
	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		// alternate snake_case
		var alt struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
		}
		if err2 := json.Unmarshal(body, &alt); err2 != nil {
			return nil, err
		}
		out.AccessToken = alt.AccessToken
		out.RefreshToken = alt.RefreshToken
		out.ExpiresIn = alt.ExpiresIn
	}
	rt := refreshToken
	if out.RefreshToken != "" {
		rt = out.RefreshToken
	}
	exp := time.Hour
	if out.ExpiresIn > 0 {
		exp = time.Duration(out.ExpiresIn) * time.Second
	}
	return &Token{
		AccessToken:  out.AccessToken,
		RefreshToken: rt,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(exp),
	}, nil
}

func refreshKiroOIDC(ctx context.Context, refreshToken string, psd map[string]any) (*Token, error) {
	clientId := "kiro-desktop"
	if psd != nil {
		if c, ok := psd["clientId"].(string); ok && c != "" {
			clientId = c
		}
	}
	region := "us-east-1"
	if psd != nil {
		if r, ok := psd["region"].(string); ok && r != "" {
			region = r
		}
	}
	tokenURL := fmt.Sprintf("https://oidc.%s.amazonaws.com/token", region)
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", clientId)
	data.Set("refresh_token", refreshToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kiro oidc refresh: %s", string(body))
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	rt := refreshToken
	if out.RefreshToken != "" {
		rt = out.RefreshToken
	}
	return &Token{
		AccessToken:  out.AccessToken,
		RefreshToken: rt,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
	}, nil
}

// StartDeviceFlowForProvider starts device flow with provider-specific quirks.
func StartDeviceFlowForProvider(ctx context.Context, provider string) (*DeviceCodeResponse, error) {
	cfg, ok := ProviderConfigs[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
	// github/copilot share device endpoint
	if provider == "copilot" {
		cfg = ProviderConfigs["github"]
	}
	return StartDeviceFlow(ctx, cfg)
}
