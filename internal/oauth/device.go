// Package oauth provides OAuth authentication flows, token lifecycle helpers,
// and specialized third-party login providers.
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

func doStartDeviceFlowRequest(ctx context.Context, config *OAuthConfig) ([]byte, error) {
	data := url.Values{}
	data.Set("client_id", config.ClientID)
	data.Set("scope", strings.Join(config.Scopes, " "))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.DeviceURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("empty device flow response")
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device flow failed: %s", string(body))
	}

	return body, nil
}

// StartDeviceFlow initiates an OAuth device authorization request.
func StartDeviceFlow(ctx context.Context, config *OAuthConfig) (*DeviceCodeResponse, error) {
	if config == nil {
		return nil, fmt.Errorf("oauth config is nil")
	}

	if config.DeviceURL == "" {
		return nil, fmt.Errorf("provider %s does not support device flow", config.Provider)
	}

	body, err := doStartDeviceFlowRequest(ctx, config)
	if err != nil {
		return nil, err
	}

	var result DeviceCodeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

type deviceTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
}

type deviceErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func doPollDeviceTokenRequest(ctx context.Context, tokenURL string, data url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return 0, nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}

	if resp == nil || resp.Body == nil {
		return 0, nil, fmt.Errorf("empty response")
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}

	return resp.StatusCode, body, nil
}

func handleDevicePollResponse(body []byte) (int, error) {
	var errResp deviceErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		if errResp.Error == "authorization_pending" {
			return 0, nil
		}

		if errResp.Error == "slow_down" {
			return 5, nil
		}

		return 0, fmt.Errorf("device flow error: %s", errResp.Error)
	}

	return 0, nil
}

// PollDeviceTokenOnce makes a single attempt to poll the token endpoint.
// Returns (token, pending, err).
func PollDeviceTokenOnce(ctx context.Context, config *OAuthConfig, deviceCode string) (*Token, bool, error) {
	if config == nil {
		return nil, false, fmt.Errorf("oauth config is nil")
	}

	data := url.Values{}
	data.Set("client_id", config.ClientID)
	data.Set("device_code", deviceCode)
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	statusCode, body, err := doPollDeviceTokenRequest(ctx, config.TokenURL, data)
	if err != nil {
		return nil, false, err
	}

	if statusCode == http.StatusOK {
		var tokenResp deviceTokenResponse
		if unmarshalErr := json.Unmarshal(body, &tokenResp); unmarshalErr != nil {
			return nil, false, unmarshalErr
		}

		return &Token{
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			TokenType:    tokenResp.TokenType,
			ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
			Scope:        tokenResp.Scope,
			IDToken:      "",
		}, false, nil
	}

	var errResp deviceErrorResponse
	if unmarshalErr := json.Unmarshal(body, &errResp); unmarshalErr == nil {
		if errResp.Error == "authorization_pending" || errResp.Error == "slow_down" {
			return nil, true, nil
		}

		if errResp.Error != "" {
			return nil, false, fmt.Errorf("%s", errResp.Error)
		}
	}

	return nil, false, fmt.Errorf("device poll unexpected status %d: %s", statusCode, string(body))
}

// PollDeviceToken polls the OAuth token endpoint until authorized or context canceled.
func PollDeviceToken(ctx context.Context, config *OAuthConfig, deviceCode string, interval int) (*Token, error) {
	if config == nil {
		return nil, fmt.Errorf("oauth config is nil")
	}

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

		statusCode, body, err := doPollDeviceTokenRequest(ctx, config.TokenURL, data)
		if err != nil {
			continue
		}

		if statusCode == http.StatusOK {
			var tokenResp deviceTokenResponse
			if unmarshalErr := json.Unmarshal(body, &tokenResp); unmarshalErr != nil {
				return nil, unmarshalErr
			}

			return &Token{
				AccessToken:  tokenResp.AccessToken,
				RefreshToken: tokenResp.RefreshToken,
				TokenType:    tokenResp.TokenType,
				ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
				Scope:        tokenResp.Scope,
				IDToken:      "",
			}, nil
		}

		incInterval, err := handleDevicePollResponse(body)
		if err != nil {
			return nil, err
		}

		interval += incInterval
	}
}
