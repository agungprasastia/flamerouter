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
	"log"
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

// NewOIDCHandler creates a new OpenID Connect authentication handler.
func NewOIDCHandler(jwt *JWTManager, st *store.Store) *OIDCHandler {
	return &OIDCHandler{
		jwt: jwt,
		st:  st,
		client: &http.Client{
			Transport:     nil,
			CheckRedirect: nil,
			Jar:           nil,
			Timeout:       15 * time.Second,
		},
	}
}

type oidcConfig struct {
	issuerURL    string
	clientID     string
	clientSecret string
	scopes       string
	redirectURI  string
	loginLabel   string
}

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

type secretProbe struct {
	valid   *bool
	message string
	tested  bool
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

func trimSlashes(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

func parseOIDCConfig(settings map[string]string) *oidcConfig {
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

func (o *OIDCHandler) loadConfig() *oidcConfig {
	if o.st == nil {
		return nil
	}

	settings, err := o.st.ListSettings()
	if err != nil {
		return nil
	}

	return parseOIDCConfig(settings)
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

	defer func() {
		if clErr := res.Body.Close(); clErr != nil {
			_ = clErr
		}
	}()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to load OIDC discovery document from %s", u)
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

func createPKCE() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}

	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	return verifier, challenge, nil
}

func randomB64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(b), nil
}

func newOIDCCookie(name, value string, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:        name,
		Value:       value,
		Path:        "/",
		Domain:      "",
		Expires:     time.Time{},
		RawExpires:  "",
		MaxAge:      maxAge,
		Secure:      secure,
		HttpOnly:    true,
		SameSite:    http.SameSiteLaxMode,
		Partitioned: false,
		Raw:         "",
		Unparsed:    nil,
		Quoted:      false,
	}
}

func setOIDCCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, newOIDCCookie(name, value, oidcCookieMaxAge, secure))
}

func clearOIDCCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, newOIDCCookie(name, "", -1, false))
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
		log.Printf("[oidc] discovery failed: %v", err)
		redirectLogin(w, r, "oidc_discovery_failed")

		return
	}

	state, errS := randomB64(16)
	nonce, errN := randomB64(16)
	verifier, challenge, errP := createPKCE()

	if errS != nil || errN != nil || errP != nil {
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

func (o *OIDCHandler) validateCallbackState(r *http.Request) (string, string, bool) {
	state := r.URL.Query().Get("state")
	storedState := cookieVal(r, oidcStateCookie)
	storedNonce := cookieVal(r, oidcNonceCookie)
	verifier := cookieVal(r, oidcVerifierCookie)

	if state == "" || storedState == "" || storedNonce == "" || verifier == "" || storedState != state {
		return "", "", false
	}

	return storedNonce, verifier, true
}

func (o *OIDCHandler) processCallbackSession(w http.ResponseWriter, r *http.Request, payload map[string]any) {
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

	http.SetCookie(w, newOIDCCookie(cookieName, token, int(sessionExpiry.Seconds()), config.AuthCookieSecure() || r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"))
	http.Redirect(w, r, publicOrigin(r)+"/dashboard", http.StatusFound)
}

func (o *OIDCHandler) resolveCallbackConfig(w http.ResponseWriter, r *http.Request) (*oidcConfig, *oidcDiscovery, bool) {
	cfg := o.loadConfig()
	if cfg == nil {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, "oidc_not_configured")

		return nil, nil, false
	}

	disc, err := o.fetchDiscovery(cfg.issuerURL)
	if err != nil {
		clearAllOIDCCookies(w)
		log.Printf("[oidc] discovery failed: %v", err)
		redirectLogin(w, r, "oidc_discovery_failed")

		return nil, nil, false
	}

	return cfg, disc, true
}

func (o *OIDCHandler) resolveIDTokenPayload(w http.ResponseWriter, r *http.Request, cfg *oidcConfig, disc *oidcDiscovery, code, verifier, storedNonce string) (map[string]any, bool) {
	redirectURI := cfg.redirectURI
	if redirectURI == "" {
		redirectURI = publicOrigin(r) + "/api/auth/oidc/callback"
	}

	tokenData, err := o.exchangeCode(disc.TokenEndpoint, cfg, code, redirectURI, verifier)
	if err != nil {
		clearAllOIDCCookies(w)
		log.Printf("[oidc] token exchange failed: %v", err)
		redirectLogin(w, r, "oidc_token_exchange_failed")

		return nil, false
	}

	idToken, ok := tokenData["id_token"].(string)
	if !ok || idToken == "" {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, "OIDC provider did not return an id_token")

		return nil, false
	}

	issuer := disc.Issuer
	if issuer == "" {
		issuer = cfg.issuerURL
	}

	payload, err := o.verifyIDToken(idToken, issuer, cfg.clientID, disc.JWKSURI, storedNonce)
	if err != nil {
		clearAllOIDCCookies(w)
		log.Printf("[oidc] id_token verification failed: %v", err)
		redirectLogin(w, r, "oidc_token_verification_failed")

		return nil, false
	}

	return payload, true
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

	storedNonce, verifier, ok := o.validateCallbackState(r)
	if !ok || code == "" {
		clearAllOIDCCookies(w)
		redirectLogin(w, r, "oidc_invalid_state")

		return
	}

	cfg, disc, ok := o.resolveCallbackConfig(w, r)
	if !ok {
		return
	}

	payload, ok := o.resolveIDTokenPayload(w, r, cfg, disc, code, verifier, storedNonce)
	if !ok {
		return
	}

	clearAllOIDCCookies(w)
	o.processCallbackSession(w, r, payload)
}

func (o *OIDCHandler) executeExchangeRequest(tokenEndpoint string, form url.Values) (*http.Response, error) {
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

	return res, nil
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

	res, err := o.executeExchangeRequest(tokenEndpoint, form)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := res.Body.Close(); clErr != nil {
			_ = clErr
		}
	}()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, extractTokenError(res.StatusCode, data)
	}

	return data, nil
}

func extractTokenError(status int, data map[string]any) error {
	msg, ok := data["error_description"].(string)
	if !ok || msg == "" {
		if errVal, ok := data["error"].(string); ok {
			msg = errVal
		}
	}

	if msg == "" {
		msg = fmt.Sprintf("OIDC token exchange failed (%d)", status)
	}

	return fmt.Errorf("%s", msg)
}

func (o *OIDCHandler) extractStoreSettings() (string, string, string, string) {
	issuer, clientID, clientSecret, scopes := "", "", "", defaultOidcScopes
	if o.st == nil {
		return issuer, clientID, clientSecret, scopes
	}

	settings, err := o.st.ListSettings()
	if err != nil {
		return issuer, clientID, clientSecret, scopes
	}

	issuer = firstNonEmpty(settings["oidcIssuerUrl"], settings["oidc.issuer"])
	clientID = firstNonEmpty(settings["oidcClientId"], settings["oidc.clientId"])
	clientSecret = firstNonEmpty(settings["oidcClientSecret"], settings["oidc.clientSecret"])

	if s := strings.TrimSpace(settings["oidcScopes"]); s != "" {
		scopes = s
	}

	return issuer, clientID, clientSecret, scopes
}

func parseTestPostBody(r *http.Request, curIssuer, curClientID, curSecret, curScopes string) (string, string, string, string) {
	if r.Method != http.MethodPost {
		return curIssuer, curClientID, curSecret, curScopes
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return curIssuer, curClientID, curSecret, curScopes
	}

	if v, ok := body["issuerUrl"].(string); ok && strings.TrimSpace(v) != "" {
		curIssuer = strings.TrimSpace(v)
	}

	if v, ok := body["clientId"].(string); ok && strings.TrimSpace(v) != "" {
		curClientID = strings.TrimSpace(v)
	}

	if v, ok := body["clientSecret"].(string); ok {
		curSecret = strings.TrimSpace(v)
	}

	if v, ok := body["scopes"].(string); ok && strings.TrimSpace(v) != "" {
		curScopes = strings.TrimSpace(v)
	}

	return curIssuer, curClientID, curSecret, curScopes
}

func (o *OIDCHandler) extractTestParams(r *http.Request) (string, string, string, string) {
	issuer, clientID, clientSecret, scopes := o.extractStoreSettings()
	issuer, clientID, clientSecret, scopes = parseTestPostBody(r, issuer, clientID, clientSecret, scopes)

	return trimSlashes(issuer), strings.TrimSpace(clientID), strings.TrimSpace(clientSecret), scopes
}

// Test handles GET|POST /api/auth/oidc/test — validates config + discovery.
func (o *OIDCHandler) Test(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	issuer, clientID, clientSecret, scopes := o.extractTestParams(r)
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
		log.Printf("[oidc] test discovery failed: %v", err)
		writeOIDCJSON(w, http.StatusInternalServerError, map[string]any{"error": "OIDC discovery failed"})

		return
	}

	redirectURI := publicOrigin(r) + "/api/auth/oidc/callback"

	if o.st != nil {
		if s, err := o.st.GetSetting("oidcRedirectUri"); err == nil && s != "" {
			redirectURI = s
		}
	}

	probe := o.probeClientSecret(disc.TokenEndpoint, clientID, clientSecret, redirectURI)
	o.renderTestResponse(w, disc, probe, issuer, clientID, scopes, redirectURI)
}

func (o *OIDCHandler) renderTestResponse(w http.ResponseWriter, disc *oidcDiscovery, probe secretProbe, issuer, clientID, scopes, redirectURI string) {
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

func isClientMismatch(errDesc string) bool {
	descLower := strings.ToLower(errDesc)
	if !strings.Contains(descLower, "client") {
		return false
	}

	return strings.Contains(descLower, "invalid") || strings.Contains(descLower, "failed") || strings.Contains(descLower, "mismatch")
}

func evaluateSecretProbe(statusCode int, errCode, errDesc string) (valid *bool, message string) {
	if statusCode >= 200 && statusCode < 300 {
		t := true
		return &t, "Client secret was accepted by the token endpoint."
	}

	errLower := strings.ToLower(errCode)
	if errLower == "invalid_client" || errLower == "unauthorized_client" || isClientMismatch(errDesc) {
		f := false
		return &f, errDesc
	}

	descLower := strings.ToLower(errDesc)
	if errLower == "invalid_grant" || errLower == "invalid_code" || strings.Contains(descLower, "grant") || strings.Contains(descLower, "code") {
		t := true
		return &t, "Client secret was accepted; the token exchange failed only because the test authorization code is invalid."
	}

	return nil, errDesc
}

func (o *OIDCHandler) probeClientSecret(tokenEndpoint, clientID, clientSecret, redirectURI string) secretProbe {
	if clientSecret == "" {
		return secretProbe{
			valid:   nil,
			message: "No client secret was provided, so secret validation was skipped.",
			tested:  false,
		}
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", "__oidc_test_invalid_code__")
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", "__oidc_test_invalid_verifier__")

	res, err := o.executeExchangeRequest(tokenEndpoint, form)
	if err != nil {
		return secretProbe{valid: nil, message: err.Error(), tested: true}
	}

	defer func() {
		if clErr := res.Body.Close(); clErr != nil {
			_ = clErr
		}
	}()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return secretProbe{valid: nil, message: err.Error(), tested: true}
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return secretProbe{valid: nil, message: err.Error(), tested: true}
	}

	errCode, ok := data["error"].(string)
	if !ok {
		errCode = ""
	}

	errDesc, ok := data["error_description"].(string)
	if !ok || errDesc == "" {
		errDesc = errCode
	}

	valid, msg := evaluateSecretProbe(res.StatusCode, errCode, errDesc)

	return secretProbe{valid: valid, message: msg, tested: true}
}

func writeOIDCJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
	}
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

func validateIDTokenClaims(claims map[string]any, issuer, audience, nonce string) error {
	if iss, ok := claims["iss"].(string); ok && iss != "" && trimSlashes(iss) != trimSlashes(issuer) {
		return fmt.Errorf("id_token issuer mismatch")
	}

	if !audMatch(claims["aud"], audience) {
		return fmt.Errorf("id_token audience mismatch")
	}

	if exp, ok := claims["exp"].(float64); ok {
		if time.Now().Unix() > int64(exp) {
			return fmt.Errorf("id_token expired")
		}
	}

	if nonce != "" {
		if n, ok := claims["nonce"].(string); !ok || n != nonce {
			return fmt.Errorf("id_token nonce mismatch")
		}
	}

	return nil
}

func verifyRSASig(k *rsa.PublicKey, alg string, sigInput, sig []byte) error {
	if alg != "" && alg != "RS256" {
		return fmt.Errorf("unsupported alg %s for RSA key", alg)
	}

	h := sha256.Sum256(sigInput)
	if err := rsa.VerifyPKCS1v15(k, crypto.SHA256, h[:], sig); err != nil {
		return fmt.Errorf("id_token signature invalid")
	}

	return nil
}

func verifyECSig(k *ecdsa.PublicKey, alg string, sigInput, sig []byte) error {
	if alg != "" && alg != "ES256" {
		return fmt.Errorf("unsupported alg %s for EC key", alg)
	}

	if len(sig) != 64 {
		return fmt.Errorf("id_token signature invalid")
	}

	rInt := new(big.Int).SetBytes(sig[:32])
	sInt := new(big.Int).SetBytes(sig[32:])
	h := sha256.Sum256(sigInput)

	if !ecdsa.Verify(k, h[:], rInt, sInt) {
		return fmt.Errorf("id_token signature invalid")
	}

	return nil
}

func verifyTokenSig(key any, alg string, sigInput, sig []byte) error {
	switch k := key.(type) {
	case *rsa.PublicKey:
		return verifyRSASig(k, alg, sigInput, sig)
	case *ecdsa.PublicKey:
		return verifyECSig(k, alg, sigInput, sig)
	default:
		return fmt.Errorf("unsupported jwks key type")
	}
}

func parseTokenParts(idToken string) (headerAlg, headerKid string, payload map[string]any, sigInput, sig []byte, err error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", "", nil, nil, nil, fmt.Errorf("invalid id_token format")
	}

	headerJSON, errH := base64.RawURLEncoding.DecodeString(parts[0])
	payloadJSON, errP := base64.RawURLEncoding.DecodeString(parts[1])
	sigBytes, errS := base64.RawURLEncoding.DecodeString(parts[2])

	if errH != nil || errP != nil || errS != nil {
		return "", "", nil, nil, nil, fmt.Errorf("invalid id_token encoding")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}

	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return "", "", nil, nil, nil, fmt.Errorf("invalid id_token header")
	}

	var claims map[string]any
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return "", "", nil, nil, nil, fmt.Errorf("invalid id_token claims")
	}

	return header.Alg, header.Kid, claims, []byte(parts[0] + "." + parts[1]), sigBytes, nil
}

func (o *OIDCHandler) verifyIDToken(idToken, issuer, audience, jwksURI, nonce string) (map[string]any, error) {
	alg, kid, claims, sigInput, sig, err := parseTokenParts(idToken)
	if err != nil {
		return nil, err
	}

	if valErr := validateIDTokenClaims(claims, issuer, audience, nonce); valErr != nil {
		return nil, valErr
	}

	if jwksURI == "" {
		return claims, nil
	}

	key, fetchErr := o.fetchJWKSKey(jwksURI, kid, alg)
	if fetchErr != nil {
		return nil, fetchErr
	}

	if sigErr := verifyTokenSig(key, alg, sigInput, sig); sigErr != nil {
		return nil, sigErr
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

func matchJWK(k jwkKey, pub any, kid, alg string) (any, bool) {
	if kid != "" && k.Kid == kid {
		return pub, true
	}

	if kid == "" && (alg == "" || k.Alg == "" || k.Alg == alg) {
		return pub, true
	}

	return nil, false
}

func (o *OIDCHandler) fetchJWKSDoc(jwksURI string) (*jwksDoc, error) {
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

	defer func() {
		if clErr := res.Body.Close(); clErr != nil {
			_ = clErr
		}
	}()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch JWKS")
	}

	var doc jwksDoc
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		return nil, err
	}

	return &doc, nil
}

func (o *OIDCHandler) fetchJWKSKey(jwksURI, kid, alg string) (any, error) {
	doc, err := o.fetchJWKSDoc(jwksURI)
	if err != nil {
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

		if matched, ok := matchJWK(k, pub, kid, alg); ok {
			return matched, nil
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

func parseRSAJWK(k jwkKey) (any, error) {
	nb, errN := base64.RawURLEncoding.DecodeString(k.N)
	if errN != nil {
		return nil, fmt.Errorf("invalid RSA jwk params")
	}

	eb, errE := base64.RawURLEncoding.DecodeString(k.E)
	if errE != nil {
		return nil, fmt.Errorf("invalid RSA jwk params")
	}

	e := 0
	for _, b := range eb {
		e = e<<8 + int(b)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nb),
		E: e,
	}, nil
}

func parseECJWK(k jwkKey) (any, error) {
	if k.Crv != "P-256" {
		return nil, fmt.Errorf("unsupported curve")
	}

	xb, errX := base64.RawURLEncoding.DecodeString(k.X)
	if errX != nil {
		return nil, fmt.Errorf("invalid EC jwk params")
	}

	yb, errY := base64.RawURLEncoding.DecodeString(k.Y)
	if errY != nil {
		return nil, fmt.Errorf("invalid EC jwk params")
	}

	return &ecdsa.PublicKey{
		Curve: elliptic.P256(),
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}, nil
}

func parseJWK(k jwkKey) (any, error) {
	switch k.Kty {
	case "RSA":
		return parseRSAJWK(k)
	case "EC":
		return parseECJWK(k)
	default:
		return nil, fmt.Errorf("unsupported kty")
	}
}
