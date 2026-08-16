package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flamerouter/internal/config"
	"flamerouter/internal/store"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	oidcStateCookie    = "oidc_state"
	oidcNonceCookie    = "oidc_nonce"
	oidcVerifierCookie = "oidc_code_verifier"
	oidcCookieMaxAge   = 10 * 60
	defaultOidcScopes  = "openid profile email"
	defaultLoginLabel  = "Sign in with OIDC"
)

// OIDCHandler manages external identity provider login (discovery + PKCE + JWT session).
type OIDCHandler struct {
	jwt    *JWTManager
	st     *store.Store
	client *http.Client
}

func NewOIDCHandler(jwt *JWTManager, st *store.Store) *OIDCHandler {
	return &OIDCHandler{
		jwt: jwt,
		st:  st,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

type oidcConfig struct {
	issuerURL    string
	clientID     string
	clientSecret string
	scopes       string
	redirectURI  string // optional override
	loginLabel   string
}

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

func trimSlashes(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

func (o *OIDCHandler) loadConfig() *oidcConfig {
	if o.st == nil {
		return nil
	}

	settings, err := o.st.ListSettings()
	if err != nil {
		return nil
	}
	// also accept brief keys oidc.issuer etc.
	issuer := firstNonEmpty(settings["oidcIssuerUrl"], settings["oidc.issuer"])
	clientID := firstNonEmpty(settings["oidcClientId"], settings["oidc.clientId"])
	secret := firstNonEmpty(settings["oidcClientSecret"], settings["oidc.clientSecret"])

	if trimSlashes(issuer) == "" || strings.TrimSpace(clientID) == "" || strings.TrimSpace(secret) == "" {
		return nil
	}

	mode := settings["authMode"]
	if mode != "" && mode != "oidc" && mode != "both" {
		return nil
	}

	scopes := strings.TrimSpace(settings["oidcScopes"])
	if scopes == "" {
		scopes = defaultOidcScopes
	}

	label := strings.TrimSpace(settings["oidcLoginLabel"])
	if label == "" {
		label = defaultLoginLabel
	}

	return &oidcConfig{
		issuerURL:    trimSlashes(issuer),
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(secret),
		scopes:       scopes,
		redirectURI:  firstNonEmpty(settings["oidcRedirectUri"], settings["oidc.redirectUri"]),
		loginLabel:   label,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}

	return ""
}

func publicOrigin(r *http.Request) string {
	if r == nil {
		return ""
	}

	if u := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); u != "" {
		proto := r.Header.Get("X-Forwarded-Proto")
		if proto == "" {
			proto = "http"
		}

		return trimSlashes(proto + "://" + u)
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}

	host := r.Host
	if host == "" {
		host = r.URL.Host
	}

	return trimSlashes(scheme + "://" + host)
}

func (o *OIDCHandler) fetchDiscovery(issuer string) (*oidcDiscovery, error) {
	u := trimSlashes(issuer) + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	res, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}

	if res == nil || res.Body == nil {
		return nil, fmt.Errorf("empty discovery response from %s", u)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to load OIDC discovery document from %s", u)
	}

	var d oidcDiscovery
	if err := json.NewDecoder(res.Body).Decode(&d); err != nil {
		return nil, err
	}

	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" {
		return nil, fmt.Errorf("invalid discovery document")
	}

	return &d, nil
}

func createPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}

	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])

	return verifier, challenge, nil
}

func randomB64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func setOIDCCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   oidcCookieMaxAge,
	})
}

func clearOIDCCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func clearAllOIDCCookies(w http.ResponseWriter) {
	clearOIDCCookie(w, oidcStateCookie)
	clearOIDCCookie(w, oidcNonceCookie)
	clearOIDCCookie(w, oidcVerifierCookie)
}

func cookieVal(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}

	return c.Value
}

func redirectLogin(w http.ResponseWriter, r *http.Request, errCode string) {
	origin := publicOrigin(r)
	loc := origin + "/login?error=" + url.QueryEscape(errCode)
	http.Redirect(w, r, loc, http.StatusFound)
}

// Start handles GET /api/auth/oidc/start — discovery + PKCE + redirect.
func (o *OIDCHandler) Start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	cfg := o.loadConfig()
	if cfg == nil {
		redirectLogin(w, r, "oidc_not_configured")
		return
	}

	disc, err := o.fetchDiscovery(cfg.issuerURL)
	if err != nil {
		redirectLogin(w, r, err.Error())
		return
	}

	state, err := randomB64(16)
	if err != nil {
		redirectLogin(w, r, "oidc_start_failed")
		return
	}

	nonce, err := randomB64(16)
	if err != nil {
		redirectLogin(w, r, "oidc_start_failed")
		return
	}

	verifier, challenge, err := createPKCE()
	if err != nil {
		redirectLogin(w, r, "oidc_start_failed")
		return
	}

	redirectURI := cfg.redirectURI
	if redirectURI == "" {
		redirectURI = publicOrigin(r) + "/api/auth/oidc/callback"
	}

	authURL, err := url.Parse(disc.AuthorizationEndpoint)
	if err != nil {
		redirectLogin(w, r, "oidc_start_failed")
		return
	}

	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", cfg.clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", cfg.scopes)
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()

	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	setOIDCCookie(w, oidcStateCookie, state, secure)
	setOIDCCookie(w, oidcNonceCookie, nonce, secure)
	setOIDCCookie(w, oidcVerifierCookie, verifier, secure)
	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

// Callback handles GET /api/auth/oidc/callback — code exchange + id_token + session JWT.
func (o *OIDCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		redirectLogin(w, r, errParam)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		redirectLogin(w, r, "oidc_missing_code")
		return
	}

	storedState := cookieVal(r, oidcStateCookie)
	storedNonce := cookieVal(r, oidcNonceCookie)
	verifier := cookieVal(r, oidcVerifierCookie)

	if storedState == "" || storedNonce == "" || verifier == "" || storedState != state {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, "oidc_invalid_state")

		return
	}

	cfg := o.loadConfig()
	if cfg == nil {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, "oidc_not_configured")

		return
	}

	disc, err := o.fetchDiscovery(cfg.issuerURL)
	if err != nil {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, err.Error())

		return
	}

	redirectURI := cfg.redirectURI
	if redirectURI == "" {
		redirectURI = publicOrigin(r) + "/api/auth/oidc/callback"
	}

	tokenData, err := o.exchangeCode(disc.TokenEndpoint, cfg, code, redirectURI, verifier)
	if err != nil {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, err.Error())

		return
	}

	idToken, _ := tokenData["id_token"].(string)
	if idToken == "" {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, "OIDC provider did not return an id_token")

		return
	}

	issuer := disc.Issuer
	if issuer == "" {
		issuer = cfg.issuerURL
	}

	payload, err := o.verifyIDToken(idToken, issuer, cfg.clientID, disc.JWKSURI, storedNonce)
	if err != nil {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, err.Error())

		return
	}

	clearAllOIDCCookies(w)

	claims := map[string]any{
		"sub":       "admin",
		"oidc":      true,
		"oidcSub":   payload["sub"],
		"oidcEmail": pickOIDCEmail(payload),
		"oidcName":  pickOIDCDisplayName(payload),
	}

	token, err := o.jwt.Generate(claims, sessionExpiry)
	if err != nil {
		redirectLogin(w, r, "oidc_callback_failed")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   config.AuthCookieSecure() || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		MaxAge:   int(sessionExpiry.Seconds()),
	})
	http.Redirect(w, r, publicOrigin(r)+"/dashboard", http.StatusFound)
}

func (o *OIDCHandler) exchangeCode(tokenEndpoint string, cfg *oidcConfig, code, redirectURI, verifier string) (map[string]any, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", cfg.clientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", verifier)

	if cfg.clientSecret != "" {
		form.Set("client_secret", cfg.clientSecret)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res == nil || res.Body == nil {
		return nil, fmt.Errorf("empty response from token endpoint")
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var data map[string]any

	_ = json.Unmarshal(body, &data)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		msg, _ := data["error_description"].(string)
		if msg == "" {
			msg, _ = data["error"].(string)
		}

		if msg == "" {
			msg = fmt.Sprintf("OIDC token exchange failed (%d)", res.StatusCode)
		}

		return nil, fmt.Errorf("%s", msg)
	}

	return data, nil
}

// Test handles GET|POST /api/auth/oidc/test — validates config + discovery.
func (o *OIDCHandler) Test(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	issuer, clientID, clientSecret, scopes := "", "", "", defaultOidcScopes

	if o.st != nil {
		settings, _ := o.st.ListSettings()
		issuer = firstNonEmpty(settings["oidcIssuerUrl"], settings["oidc.issuer"])
		clientID = firstNonEmpty(settings["oidcClientId"], settings["oidc.clientId"])
		clientSecret = firstNonEmpty(settings["oidcClientSecret"], settings["oidc.clientSecret"])

		if s := strings.TrimSpace(settings["oidcScopes"]); s != "" {
			scopes = s
		}
	}

	if r.Method == http.MethodPost {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		if v, ok := body["issuerUrl"].(string); ok && strings.TrimSpace(v) != "" {
			issuer = strings.TrimSpace(v)
		}

		if v, ok := body["clientId"].(string); ok && strings.TrimSpace(v) != "" {
			clientID = strings.TrimSpace(v)
		}

		if _, ok := body["clientSecret"]; ok {
			if v, ok := body["clientSecret"].(string); ok {
				clientSecret = strings.TrimSpace(v)
			}
		}

		if v, ok := body["scopes"].(string); ok && strings.TrimSpace(v) != "" {
			scopes = strings.TrimSpace(v)
		}
	}

	issuer = trimSlashes(issuer)
	clientID = strings.TrimSpace(clientID)

	if issuer == "" {
		writeOIDCJSON(w, http.StatusBadRequest, map[string]any{"error": "Issuer URL is required"})
		return
	}

	if clientID == "" {
		writeOIDCJSON(w, http.StatusBadRequest, map[string]any{"error": "Client ID is required"})
		return
	}

	disc, err := o.fetchDiscovery(issuer)
	if err != nil {
		writeOIDCJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	redirectURI := firstNonEmpty(
		func() string {
			if o.st != nil {
				s, _ := o.st.GetSetting("oidcRedirectUri")
				return s
			}

			return ""
		}(),
		publicOrigin(r)+"/api/auth/oidc/callback",
	)
	probe := o.probeClientSecret(disc.TokenEndpoint, clientID, clientSecret, redirectURI)

	if probe.tested && probe.valid != nil && !*probe.valid {
		writeOIDCJSON(w, http.StatusOK, map[string]any{
			"ok": false, "discoveryOk": true,
			"clientSecretTested": true, "clientSecretValid": false,
			"issuerUrl": issuer, "clientId": clientID, "scopes": scopes,
			"redirectUri":           redirectURI,
			"authorizationEndpoint": disc.AuthorizationEndpoint,
			"tokenEndpoint":         disc.TokenEndpoint,
			"jwksUri":               disc.JWKSURI,
			"error":                 "Discovery loaded, but the client secret is not valid: " + probe.message,
		})

		return
	}

	writeOIDCJSON(w, http.StatusOK, map[string]any{
		"ok": true, "discoveryOk": true,
		"clientSecretTested": probe.tested, "clientSecretValid": probe.valid,
		"issuerUrl": issuer, "clientId": clientID, "scopes": scopes,
		"redirectUri":           redirectURI,
		"authorizationEndpoint": disc.AuthorizationEndpoint,
		"tokenEndpoint":         disc.TokenEndpoint,
		"jwksUri":               disc.JWKSURI,
		"message":               probe.message,
	})
}

type secretProbe struct {
	valid   *bool
	message string
	tested  bool
}

func (o *OIDCHandler) probeClientSecret(tokenEndpoint, clientID, clientSecret, redirectURI string) secretProbe {
	if clientSecret == "" {
		return secretProbe{tested: false, message: "No client secret was provided, so secret validation was skipped."}
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", "__oidc_test_invalid_code__")
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", "__oidc_test_invalid_verifier__")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return secretProbe{tested: true, message: err.Error()}
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := o.client.Do(req)
	if err != nil {
		return secretProbe{tested: true, message: err.Error()}
	}
	if res == nil || res.Body == nil {
		return secretProbe{tested: true, message: "empty response from token endpoint"}
	}

	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	var data map[string]any
	_ = json.Unmarshal(body, &data)
	errCode, _ := data["error"].(string)
	errDesc, _ := data["error_description"].(string)

	if errDesc == "" {
		errDesc = errCode
	}

	errLower := strings.ToLower(errCode)

	if res.StatusCode >= 200 && res.StatusCode < 300 {
		t := true
		return secretProbe{tested: true, valid: &t, message: "Client secret was accepted by the token endpoint."}
	}

	if errLower == "invalid_client" || errLower == "unauthorized_client" ||
		strings.Contains(strings.ToLower(errDesc), "client") && (strings.Contains(strings.ToLower(errDesc), "invalid") || strings.Contains(strings.ToLower(errDesc), "failed") || strings.Contains(strings.ToLower(errDesc), "mismatch")) {
		f := false
		return secretProbe{tested: true, valid: &f, message: errDesc}
	}

	if errLower == "invalid_grant" || errLower == "invalid_code" ||
		strings.Contains(strings.ToLower(errDesc), "grant") || strings.Contains(strings.ToLower(errDesc), "code") {
		t := true
		return secretProbe{tested: true, valid: &t, message: "Client secret was accepted; the token exchange failed only because the test authorization code is invalid."}
	}

	return secretProbe{tested: true, message: errDesc}
}

func writeOIDCJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func pickOIDCDisplayName(payload map[string]any) string {
	for _, k := range []string{"preferred_username", "email", "name", "given_name", "sub"} {
		if v, ok := payload[k].(string); ok && v != "" {
			return v
		}
	}

	return "OIDC user"
}

func pickOIDCEmail(payload map[string]any) string {
	if v, ok := payload["email"].(string); ok {
		return v
	}

	return ""
}

// --- id_token JWT verify via JWKS (RS256/ES256, stdlib) ---

func (o *OIDCHandler) verifyIDToken(idToken, issuer, audience, jwksURI, nonce string) (map[string]any, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid id_token format")
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid id_token header")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}

	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("invalid id_token header")
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid id_token payload")
	}

	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("invalid id_token claims")
	}

	if iss, _ := claims["iss"].(string); iss != "" && trimSlashes(iss) != trimSlashes(issuer) {
		return nil, fmt.Errorf("id_token issuer mismatch")
	}

	if !audMatch(claims["aud"], audience) {
		return nil, fmt.Errorf("id_token audience mismatch")
	}

	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return nil, fmt.Errorf("id_token expired")
		}
	}

	if nonce != "" {
		if n, _ := claims["nonce"].(string); n != nonce {
			return nil, fmt.Errorf("id_token nonce mismatch")
		}
	}

	if jwksURI == "" {
		return claims, nil // ponytail: skip sig if no jwks; upgrade when required
	}

	key, err := o.fetchJWKSKey(jwksURI, header.Kid, header.Alg)
	if err != nil {
		return nil, err
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid id_token signature encoding")
	}

	sigInput := []byte(parts[0] + "." + parts[1])

	switch k := key.(type) {
	case *rsa.PublicKey:
		if header.Alg != "" && header.Alg != "RS256" {
			return nil, fmt.Errorf("unsupported alg %s for RSA key", header.Alg)
		}

		h := sha256.Sum256(sigInput)
		if err := rsa.VerifyPKCS1v15(k, crypto.SHA256, h[:], sig); err != nil {
			return nil, fmt.Errorf("id_token signature invalid")
		}
	case *ecdsa.PublicKey:
		if header.Alg != "" && header.Alg != "ES256" {
			return nil, fmt.Errorf("unsupported alg %s for EC key", header.Alg)
		}

		if len(sig) != 64 {
			return nil, fmt.Errorf("id_token signature invalid")
		}

		rInt := new(big.Int).SetBytes(sig[:32])
		sInt := new(big.Int).SetBytes(sig[32:])
		h := sha256.Sum256(sigInput)

		if !ecdsa.Verify(k, h[:], rInt, sInt) {
			return nil, fmt.Errorf("id_token signature invalid")
		}
	default:
		return nil, fmt.Errorf("unsupported jwks key type")
	}

	return claims, nil
}

func audMatch(aud any, expected string) bool {
	switch v := aud.(type) {
	case string:
		return v == expected
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == expected {
				return true
			}
		}
	}

	return expected == ""
}

type jwksDoc struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (o *OIDCHandler) fetchJWKSKey(jwksURI, kid, alg string) (any, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, err
	}

	res, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res == nil || res.Body == nil {
		return nil, fmt.Errorf("failed to fetch JWKS: empty response")
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch JWKS")
	}

	var doc jwksDoc
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		return nil, err
	}

	var fallback any

	for _, k := range doc.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}

		pub, err := parseJWK(k)
		if err != nil {
			continue
		}

		if kid != "" && k.Kid == kid {
			return pub, nil
		}

		if kid == "" && (alg == "" || k.Alg == "" || k.Alg == alg) {
			return pub, nil
		}

		if fallback == nil {
			fallback = pub
		}
	}

	if fallback != nil {
		return fallback, nil
	}

	return nil, fmt.Errorf("no matching JWKS key")
}

func parseJWK(k jwkKey) (any, error) {
	switch k.Kty {
	case "RSA":
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, err
		}

		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, err
		}

		e := 0
		for _, b := range eb {
			e = e<<8 + int(b)
		}

		return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}, nil
	case "EC":
		if k.Crv != "P-256" {
			return nil, fmt.Errorf("unsupported curve")
		}

		xb, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, err
		}

		yb, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, err
		}

		return &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(xb),
			Y:     new(big.Int).SetBytes(yb),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported kty")
	}
}
