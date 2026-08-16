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

func StartDeviceFlow(ctx context.Context, config *OAuthConfig) (*DeviceCodeResponse, error) {
	if config.DeviceURL == "" {
		return nil, fmt.Errorf("provider %s does not support device flow", config.Provider)
	}

	data := url.Values{}
	data.Set("client_id", config.ClientID)
	data.Set("scope", strings.Join(config.Scopes, " "))

	req, err := http.NewRequestWithContext(ctx, "POST", config.DeviceURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device flow failed: %s", string(body))
	}

	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

func PollDeviceToken(ctx context.Context, config *OAuthConfig, deviceCode string, interval int) (*Token, error) {
	data := url.Values{}
	data.Set("client_id", config.ClientID)
	data.Set("device_code", deviceCode)
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		req, err := http.NewRequestWithContext(ctx, "POST", config.TokenURL, strings.NewReader(data.Encode()))
		if err != nil {
			return nil, err
		}

		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var tokenResp struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				TokenType    string `json:"token_type"`
				Scope        string `json:"scope"`
				ExpiresIn    int    `json:"expires_in"`
			}

			if err := json.Unmarshal(body, &tokenResp); err != nil {
				return nil, err
			}

			return &Token{
				AccessToken:  tokenResp.AccessToken,
				RefreshToken: tokenResp.RefreshToken,
				TokenType:    tokenResp.TokenType,
				ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
				Scope:        tokenResp.Scope,
			}, nil
		}

		var errResp struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}

		if err := json.Unmarshal(body, &errResp); err == nil {
			if errResp.Error == "authorization_pending" {
				continue
			}

			if errResp.Error == "slow_down" {
				interval += 5
				continue
			}

			return nil, fmt.Errorf("device flow error: %s", errResp.Error)
		}
	}
}
