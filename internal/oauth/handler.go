package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"flamerouter/internal/store"
)

type Handler struct {
	states map[string]*OAuthState
}

func NewHandler() *Handler {
	return &Handler{
		states: make(map[string]*OAuthState),
	}
}

func (h *Handler) StartAuth(w http.ResponseWriter, r *http.Request, provider string) {
	config, ok := ProviderConfigs[provider]
	if !ok {
		http.Error(w, `{"error":"unknown provider"}`, http.StatusBadRequest)
		return
	}

	state, err := GenerateState()
	if err != nil {
		http.Error(w, `{"error":"failed to generate state"}`, http.StatusInternalServerError)
		return
	}

	codeVerifier, err := GenerateCodeVerifier()
	if err != nil {
		http.Error(w, `{"error":"failed to generate code verifier"}`, http.StatusInternalServerError)
		return
	}

	redirectURI := config.RedirectURL
	if rr := r.URL.Query().Get("redirect_uri"); rr != "" {
		redirectURI = rr
	}

	h.states[state] = &OAuthState{
		State:       state,
		Provider:    provider,
		RedirectURI: redirectURI,
		CreatedAt:   time.Now(),
	}

	params := url.Values{}
	params.Set("client_id", config.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(config.Scopes, " "))
	params.Set("state", state)

	if config.AuthStyle == "pkce" {
		challenge := GenerateCodeChallenge(codeVerifier)
		params.Set("code_challenge", challenge)
		params.Set("code_challenge_method", "S256")
	}

	authURL := config.AuthURL + "?" + params.Encode()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"authUrl":       authURL,
		"auth_url":      authURL,
		"state":         state,
		"codeVerifier":  codeVerifier,
		"code_verifier": codeVerifier,
	})
}

func (h *Handler) HandleCallback(w http.ResponseWriter, r *http.Request, provider string) {
	config, ok := ProviderConfigs[provider]
	if !ok {
		http.Error(w, `{"error":"unknown provider"}`, http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Error(w, `{"error":"missing code or state"}`, http.StatusBadRequest)
		return
	}

	oauthState, ok := h.states[state]
	if !ok || oauthState.Provider != provider {
		http.Error(w, `{"error":"invalid state"}`, http.StatusBadRequest)
		return
	}
	delete(h.states, state)

	token, err := h.exchangeCode(r.Context(), config, code, oauthState.RedirectURI, "")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"token exchange failed: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"expires_at":    token.ExpiresAt,
		"token_type":    token.TokenType,
	})
}

// ExchangeAndSave exchanges auth code (or raw JWT access token) and stores connection.
func (h *Handler) ExchangeAndSave(ctx context.Context, st *store.Store, provider, code, redirectURI, codeVerifier, state string, meta map[string]any) (map[string]any, error) {
	if code == "" {
		return nil, fmt.Errorf("missing code")
	}
	// Raw JWT access token (codex website paste)
	if strings.HasPrefix(code, "eyJ") && strings.Contains(code, ".") {
		psd := map[string]any{"authMethod": "access_token"}
		if info := decodeJWTClaims(code); info != nil {
			if v, ok := info["account_id"].(string); ok && v != "" {
				psd["chatgptAccountId"] = v
			}
			if v, ok := info["plan_type"].(string); ok && v != "" {
				psd["chatgptPlanType"] = v
			}
		}
		email := ""
		if info := decodeJWTClaims(code); info != nil {
			if v, ok := info["email"].(string); ok {
				email = v
			}
		}
		name := email
		if name == "" {
			name = provider
		}
		id, err := st.CreateOAuthConnection(provider, "access_token", name, code, "", "", psd)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "provider": provider, "email": email, "displayName": name}, nil
	}

	noPKCE := provider == "cline" || provider == "clinepass" || provider == "kimchi"
	if redirectURI == "" || (!noPKCE && codeVerifier == "") {
		return nil, fmt.Errorf("missing required fields")
	}
	config, ok := ProviderConfigs[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider")
	}
	// meta may override client credentials (gitlab)
	if meta != nil {
		if cid, ok := meta["clientId"].(string); ok && cid != "" {
			c2 := *config
			c2.ClientID = cid
			if sec, ok := meta["clientSecret"].(string); ok {
				c2.ClientSecret = sec
			}
			config = &c2
		}
	}
	token, err := h.exchangeCode(ctx, config, code, redirectURI, codeVerifier)
	if err != nil {
		return nil, err
	}
	expiresAt := ""
	if !token.ExpiresAt.IsZero() {
		expiresAt = token.ExpiresAt.UTC().Format(time.RFC3339)
	}

	email := ""
	name := provider
	var psd map[string]any

	// Extract email and info from ID token if present
	if token.IDToken != "" {
		if claims := decodeJWTClaims(token.IDToken); claims != nil {
			if em, ok := claims["email"].(string); ok && em != "" {
				email = em
				name = em
			}
			if nm, ok := claims["name"].(string); ok && nm != "" && name == provider {
				name = nm
			}
		}
	}

	// Antigravity post-exchange setup (fetch userinfo and project/tier)
	if provider == "antigravity" {
		psd = map[string]any{
			"tierId": "legacy-tier",
		}
		// If email not in ID token, query userinfo
		if email == "" {
			req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v1/userinfo?alt=json", nil)
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+token.AccessToken)
				if resp, err := http.DefaultClient.Do(req); err == nil {
					defer resp.Body.Close()
					var uinfo map[string]any
					if json.NewDecoder(resp.Body).Decode(&uinfo) == nil {
						if em, ok := uinfo["email"].(string); ok && em != "" {
							email = em
							name = em
						}
					}
				}
			}
		}
		// Load Code Assist to get project ID and tier
		loadReqBody := `{"metadata":{"ideType":9,"platform":1,"pluginType":2}}`
		req, err := http.NewRequestWithContext(ctx, "POST", "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", strings.NewReader(loadReqBody))
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "antigravity/ide/2.1.1 darwin/arm64")
			req.Header.Set("x-request-source", "local")
			if resp, err := http.DefaultClient.Do(req); err == nil {
				defer resp.Body.Close()
				var loadData struct {
					CloudAICompanionProject any `json:"cloudaicompanionProject"`
					AllowedTiers            []struct {
						ID        string `json:"id"`
						IsDefault bool   `json:"isDefault"`
					} `json:"allowedTiers"`
				}
				if json.NewDecoder(resp.Body).Decode(&loadData) == nil {
					var projID string
					switch p := loadData.CloudAICompanionProject.(type) {
					case string:
						projID = p
					case map[string]any:
						if id, ok := p["id"].(string); ok {
							projID = id
						}
					}
					if projID != "" {
						psd["projectId"] = projID
					}
					for _, tier := range loadData.AllowedTiers {
						if tier.IsDefault && tier.ID != "" {
							psd["tierId"] = strings.TrimSpace(tier.ID)
							break
						}
					}
				}
			}
		}
	}

	id, err := st.CreateOAuthConnection(provider, "oauth", name, token.AccessToken, token.RefreshToken, expiresAt, psd)
	if err != nil {
		return nil, err
	}
	_ = state
	return map[string]any{"id": id, "provider": provider, "email": email, "displayName": name}, nil
}

func decodeJWTClaims(tok string) map[string]any {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return nil
	}
	b64 := parts[1]
	// base64url → raw
	switch len(b64) % 4 {
	case 2:
		b64 += "=="
	case 3:
		b64 += "="
	}
	b64 = strings.ReplaceAll(strings.ReplaceAll(b64, "-", "+"), "_", "/")
	raw, err := decodeB64(b64)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

func decodeB64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

func (h *Handler) exchangeCode(ctx context.Context, config *OAuthConfig, code, redirectURI, codeVerifier string) (*Token, error) {
	if redirectURI == "" {
		redirectURI = config.RedirectURL
	}
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("client_id", config.ClientID)
	if config.ClientSecret != "" {
		data.Set("client_secret", config.ClientSecret)
	}
	if codeVerifier != "" {
		data.Set("code_verifier", codeVerifier)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", config.TokenURL, strings.NewReader(data.Encode()))
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
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		IDToken      string `json:"id_token"`
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
		IDToken:      tokenResp.IDToken,
	}, nil
}

func (h *Handler) RefreshToken(ctx context.Context, provider string, refreshToken string) (*Token, error) {
	config, ok := ProviderConfigs[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}

	if config.RefreshURL == "" {
		return nil, fmt.Errorf("provider %s does not support refresh", provider)
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", config.ClientID)
	if config.ClientSecret != "" {
		data.Set("client_secret", config.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", config.RefreshURL, strings.NewReader(data.Encode()))
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
		return nil, fmt.Errorf("refresh failed: %s", string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	newRefreshToken := refreshToken
	if tokenResp.RefreshToken != "" {
		newRefreshToken = tokenResp.RefreshToken
	}

	return &Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: newRefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		Scope:        tokenResp.Scope,
	}, nil
}
