// Package oauth provides OAuth authentication flows, token lifecycle helpers,
// and specialized third-party login providers.
package oauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Handler manages OAuth flow endpoints, token exchange, and persistence.
type Handler struct {
	states map[string]*OAuthState
}

// NewHandler constructs an OAuth Handler.
func NewHandler() *Handler {
	return &Handler{
		states: make(map[string]*OAuthState),
	}
}

// StartAuth generates PKCE challenge and state, returning an authorization URL.
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
		CreatedAt:   time.Now(),
		State:       state,
		Provider:    provider,
		RedirectURI: redirectURI,
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

	if err := json.NewEncoder(w).Encode(map[string]any{
		"authUrl":       authURL,
		"auth_url":      authURL,
		"state":         state,
		"codeVerifier":  codeVerifier,
		"code_verifier": codeVerifier,
	}); err != nil {
		http.Error(w, `{"error":"failed to encode response"}`, http.StatusInternalServerError)
	}
}

// HandleCallback handles OAuth provider callback redirects.
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

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"access_token":  token.AccessToken,
		"refresh_token": token.RefreshToken,
		"expires_at":    token.ExpiresAt,
		"token_type":    token.TokenType,
	}); err != nil {
		http.Error(w, `{"error":"failed to encode response"}`, http.StatusInternalServerError)
	}
}

func (h *Handler) saveRawJWT(st *store.Store, provider, code string) (map[string]any, error) {
	psd := map[string]any{"authMethod": "access_token"}
	email, name := ExtractIdentityFromJWT(code, provider)

	if info := DecodeJWTClaims(code); info != nil {
		if v, ok := info["account_id"].(string); ok && v != "" {
			psd["chatgptAccountId"] = v
		}

		if v, ok := info["plan_type"].(string); ok && v != "" {
			psd["chatgptPlanType"] = v
		}

		if auth, ok := info["https://api.openai.com/auth"].(map[string]any); ok {
			if v, ok := auth["chatgpt_account_id"].(string); ok && v != "" {
				psd["chatgptAccountId"] = v
			}

			if v, ok := auth["chatgpt_plan_type"].(string); ok && v != "" {
				psd["chatgptPlanType"] = v
			}
		}

		if exp, ok := info["exp"].(float64); ok && exp > 0 {
			psd["jwtExp"] = int64(exp)
		}
	}

	if email != "" {
		psd["email"] = email

		if name == "" || name == provider {
			name = email
		}
	}

	if name == "" {
		name = provider
	}

	id, err := st.CreateOAuthConnection(provider, "access_token", name, code, "", "", psd)
	if err != nil {
		return nil, err
	}

	return map[string]any{"id": id, "provider": provider, "email": email, "displayName": name}, nil
}

func resolveConfig(provider string, meta map[string]any) (*OAuthConfig, error) {
	config, ok := ProviderConfigs[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider")
	}

	if meta != nil {
		if cid, ok := meta["clientId"].(string); ok && cid != "" {
			c2 := *config
			c2.ClientID = cid

			if sec, ok := meta["clientSecret"].(string); ok {
				c2.ClientSecret = sec
			}

			return &c2, nil
		}
	}

	return config, nil
}

func extractOpenAIEmail(claims map[string]any) string {
	if prof, ok := claims["https://api.openai.com/profile"].(map[string]any); ok {
		if em, ok := prof["email"].(string); ok && em != "" {
			return em
		}
	}

	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if em, ok := auth["email"].(string); ok && em != "" {
			return em
		}

		if pref, ok := auth["preferred_username"].(string); ok && strings.Contains(pref, "@") {
			return pref
		}
	}

	return ""
}

// ExtractIdentityFromJWT parses any JWT string (id_token, access_token) and extracts email and display name.
func ExtractIdentityFromJWT(tok, defaultName string) (string, string) {
	if tok == "" {
		return "", defaultName
	}

	claims := DecodeJWTClaims(tok)
	if claims == nil {
		return "", defaultName
	}

	email := ""
	name := defaultName

	// 1. Direct email claim
	if em, ok := claims["email"].(string); ok && em != "" {
		email = em
	}

	// 2. OpenAI JWT claims (https://api.openai.com/profile or https://api.openai.com/auth)
	if email == "" {
		email = extractOpenAIEmail(claims)
	}

	// 3. Preferred username / unique_name / upn
	if email == "" {
		for _, key := range []string{"preferred_username", "unique_name", "upn"} {
			if val, ok := claims[key].(string); ok && strings.Contains(val, "@") {
				email = val
				break
			}
		}
	}

	// 4. Sub if formatted like email
	if email == "" {
		if sub, ok := claims["sub"].(string); ok && strings.Contains(sub, "@") {
			email = sub
		}
	}

	// Extract display name
	if nm, ok := claims["name"].(string); ok && nm != "" {
		name = nm
	} else if pref, ok := claims["preferred_username"].(string); ok && pref != "" {
		name = pref
	} else if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if pref, ok := auth["preferred_username"].(string); ok && pref != "" {
			name = pref
		}
	} else if email != "" {
		name = email
	}

	return email, name
}

// ExtractIdentityFromIDToken extracts email and display name from an ID token (JWT).
func ExtractIdentityFromIDToken(idToken, provider string) (string, string) {
	return ExtractIdentityFromJWT(idToken, provider)
}

// FetchProviderUserProfile attempts to fetch user identity (email/name) for providers that require an API call.
func FetchProviderUserProfile(ctx context.Context, provider, accessToken string) (string, string) {
	if accessToken == "" {
		return "", ""
	}

	client := &http.Client{
		Timeout:       5 * time.Second,
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
	}

	switch provider {
	case "grok-cli":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://cli-chat-proxy.grok.com/v1/user", nil)
		if err != nil {
			return "", ""
		}

		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "grok-shell/0.2.99")
		req.Header.Set("x-xai-token-auth", "xai-grok-cli")
		req.Header.Set("x-grok-client-identifier", "grok-shell")
		req.Header.Set("x-grok-client-version", "0.2.99")
		req.Header.Set("x-grok-client-mode", "headless")

		resp, err := client.Do(req)
		if err != nil || resp == nil || resp.Body == nil {
			return "", ""
		}

		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				_ = closeErr
			}
		}()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var res map[string]any
			if json.NewDecoder(resp.Body).Decode(&res) == nil {
				if user, ok := res["user"].(map[string]any); ok {
					em, _ := user["email"].(string) //nolint:errcheck
					nm, _ := user["name"].(string)  //nolint:errcheck

					if nm == "" {
						nm, _ = user["display_name"].(string) //nolint:errcheck
					}

					if nm == "" {
						nm, _ = user["username"].(string) //nolint:errcheck
					}

					return em, nm
				}

				em, _ := res["email"].(string) //nolint:errcheck
				nm, _ := res["name"].(string)  //nolint:errcheck

				return em, nm
			}
		}
	case "antigravity":
		em := fetchAntigravityUserInfo(ctx, accessToken)
		return em, ""
	}

	return "", ""
}

func fetchAntigravityUserInfo(ctx context.Context, accessToken string) string {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v1/userinfo?alt=json", nil)
	if err != nil {
		return ""
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp == nil || resp.Body == nil {
		return ""
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	var uinfo map[string]any
	if json.NewDecoder(resp.Body).Decode(&uinfo) == nil {
		if em, ok := uinfo["email"].(string); ok {
			return em
		}
	}

	return ""
}

type antigravityLoadData struct {
	CloudAICompanionProject any `json:"cloudaicompanionProject"`
	AllowedTiers            []struct {
		ID        string `json:"id"`
		IsDefault bool   `json:"isDefault"`
	} `json:"allowedTiers"`
}

func parseAntigravityProjectAndTier(loadData *antigravityLoadData, psd map[string]any) {
	switch p := loadData.CloudAICompanionProject.(type) {
	case string:
		psd["projectId"] = p
	case map[string]any:
		if id, ok := p["id"].(string); ok {
			psd["projectId"] = id
		}
	}

	for _, tier := range loadData.AllowedTiers {
		if tier.IsDefault && tier.ID != "" {
			psd["tierId"] = strings.TrimSpace(tier.ID)
			break
		}
	}
}

func fetchAntigravityDetails(ctx context.Context, accessToken string, psd map[string]any) {
	loadReqBody := `{"metadata":{"ideType":9,"platform":1,"pluginType":2}}`

	req, err := http.NewRequestWithContext(ctx, "POST", "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist", strings.NewReader(loadReqBody))
	if err != nil {
		return
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "antigravity/ide/2.1.1 darwin/arm64")
	req.Header.Set("x-request-source", "local")

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp == nil || resp.Body == nil {
		return
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	var loadData antigravityLoadData
	if json.NewDecoder(resp.Body).Decode(&loadData) != nil {
		return
	}

	parseAntigravityProjectAndTier(&loadData, psd)
}

func (h *Handler) setupAntigravity(ctx context.Context, token *Token, email, name string) (string, string, map[string]any) {
	psd := map[string]any{
		"tierId": "legacy-tier",
	}

	if email == "" {
		if fetchedEmail := fetchAntigravityUserInfo(ctx, token.AccessToken); fetchedEmail != "" {
			email = fetchedEmail
			name = fetchedEmail
		}
	}

	fetchAntigravityDetails(ctx, token.AccessToken, psd)

	return email, name, psd
}

func validateExchangeParams(provider, code, redirectURI, codeVerifier string) error {
	if code == "" {
		return fmt.Errorf("missing code")
	}

	noPKCE := provider == "cline" || provider == "clinepass" || provider == "kimchi"
	if redirectURI == "" || (!noPKCE && codeVerifier == "") {
		return fmt.Errorf("missing required fields")
	}

	return nil
}

func enrichCodexPSD(psd map[string]any, token *Token) {
	for _, tok := range []string{token.IDToken, token.AccessToken} {
		if tok == "" {
			continue
		}

		claims := DecodeJWTClaims(tok)
		if claims == nil {
			continue
		}

		auth, ok := claims["https://api.openai.com/auth"].(map[string]any)
		if !ok {
			continue
		}

		if v, ok := auth["chatgpt_account_id"].(string); ok && v != "" {
			psd["chatgptAccountId"] = v
		}

		if v, ok := auth["chatgpt_plan_type"].(string); ok && v != "" {
			psd["chatgptPlanType"] = v
		}
	}
}

// ExchangeAndSave exchanges auth code (or raw JWT access token) and stores connection.
func (h *Handler) ExchangeAndSave(ctx context.Context, st *store.Store, provider, code, redirectURI, codeVerifier, state string, meta map[string]any) (map[string]any, error) {
	_ = state

	if strings.HasPrefix(code, "eyJ") && strings.Contains(code, ".") {
		return h.saveRawJWT(st, provider, code)
	}

	if err := validateExchangeParams(provider, code, redirectURI, codeVerifier); err != nil {
		return nil, err
	}

	config, err := resolveConfig(provider, meta)
	if err != nil {
		return nil, err
	}

	token, err := h.exchangeCode(ctx, config, code, redirectURI, codeVerifier)
	if err != nil {
		return nil, err
	}

	expiresAt := ""
	if !token.ExpiresAt.IsZero() {
		expiresAt = token.ExpiresAt.UTC().Format(time.RFC3339)
	}

	email, name := resolveTokenIdentity(ctx, token, provider)

	var psd map[string]any

	if provider == "antigravity" {
		email, name, psd = h.setupAntigravity(ctx, token, email, name)
	}

	if psd == nil {
		psd = make(map[string]any)
	}

	if token.IDToken != "" {
		psd["idToken"] = token.IDToken
	}

	if provider == "codex" {
		enrichCodexPSD(psd, token)
	}

	if email != "" {
		psd["email"] = email
	}

	id, err := st.CreateOAuthConnection(provider, "oauth", name, token.AccessToken, token.RefreshToken, expiresAt, psd)
	if err != nil {
		return nil, err
	}

	return map[string]any{"id": id, "provider": provider, "email": email, "displayName": name}, nil
}

func resolveTokenIdentity(ctx context.Context, token *Token, provider string) (string, string) {
	email, name := ExtractIdentityFromIDToken(token.IDToken, provider)
	if email == "" && token.AccessToken != "" {
		jwtEmail, jwtName := ExtractIdentityFromJWT(token.AccessToken, provider)
		if jwtEmail != "" {
			email = jwtEmail
		}

		if jwtName != "" && (name == "" || name == provider) {
			name = jwtName
		}
	}

	if email == "" {
		fetchedEmail, fetchedName := FetchProviderUserProfile(ctx, provider, token.AccessToken)
		if fetchedEmail != "" {
			email = fetchedEmail
		}

		if fetchedName != "" && (name == "" || name == provider) {
			name = fetchedName
		}
	}

	if name == "" || name == provider {
		if email != "" {
			name = email
		} else {
			name = provider + " Connection"
		}
	}

	return email, name
}

// DecodeJWTClaims parses unverified claims from a JWT token string.
func DecodeJWTClaims(tok string) map[string]any {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return nil
	}

	b64 := parts[1]
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

type tokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func buildExchangePayload(config *OAuthConfig, code, redirectURI, codeVerifier string) url.Values {
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

	return data
}

func doExchangeRequest(ctx context.Context, tokenURL string, data url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data.Encode()))
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
		return nil, fmt.Errorf("empty token exchange response")
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
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	return body, nil
}

func (h *Handler) exchangeCode(ctx context.Context, config *OAuthConfig, code, redirectURI, codeVerifier string) (*Token, error) {
	data := buildExchangePayload(config, code, redirectURI, codeVerifier)

	body, err := doExchangeRequest(ctx, config.TokenURL, data)
	if err != nil {
		return nil, err
	}

	var tokenResp tokenExchangeResponse
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

func doTokenRefreshRequest(ctx context.Context, config *OAuthConfig, refreshToken string) ([]byte, error) {
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

	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("empty refresh response")
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
		return nil, fmt.Errorf("refresh failed: %s", string(body))
	}

	return body, nil
}

// RefreshToken refreshes an OAuth token for a given provider.
func (h *Handler) RefreshToken(ctx context.Context, provider string, refreshToken string) (*Token, error) {
	config, ok := ProviderConfigs[provider]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}

	if config.RefreshURL == "" {
		return nil, fmt.Errorf("provider %s does not support refresh", provider)
	}

	body, err := doTokenRefreshRequest(ctx, config, refreshToken)
	if err != nil {
		return nil, err
	}

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
		IDToken:      "",
	}, nil
}
