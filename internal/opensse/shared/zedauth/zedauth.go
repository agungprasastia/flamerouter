// Package zedauth provides RSA cryptographic key management and token helpers for Zed authentication.
package zedauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Constants for Zed authentication and endpoints.
const (
	PrivateKeyPrefix = "zed-rsa-pkcs1:"
	ZedWebBaseURL    = "https://zed.dev"
	ZedCloudBaseURL  = "https://cloud.zed.dev"
	ZedLLMBaseURL    = "https://cloud.zed.dev"
	// #nosec G101 -- public API URL constant, not a secret
	DefaultLLMTokenURL = "https://cloud.zed.dev/client/llm_tokens"
)

// ZedHeaders map containing standard Zed header keys.
var ZedHeaders = map[string]string{
	"expiredToken":              "x-zed-expired-token",
	"outdatedToken":             "x-zed-outdated-token",
	"clientSupportsStatus":      "x-zed-client-supports-status-messages",
	"clientSupportsStreamEnded": "x-zed-client-supports-stream-ended-request-completion-status",
	"serverSupportsStatus":      "x-zed-server-supports-status-messages",
	"clientSupportsXai":         "x-zed-client-supports-x-ai",
	"systemId":                  "x-zed-system-id",
}

// ZED_HEADERS is a deprecated alias for ZedHeaders.
//
//nolint:gochecknoglobals,stylecheck // kept for compatibility
var ZED_HEADERS = ZedHeaders

// ZedKeypair holds the RSA keypair generated for Zed authentication.
type ZedKeypair struct {
	PrivateKey    *rsa.PrivateKey
	PublicKey     *rsa.PublicKey
	PrivateKeyPEM string
	Verifier      string
	PublicKeyDER  []byte
}

// GenerateZedKeypair generates a 2048-bit RSA private key and returns PKCS#1 PEM string prefixed with "zed-rsa-pkcs1:".
func GenerateZedKeypair() (*ZedKeypair, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate rsa key: %w", err)
	}

	privDER := x509.MarshalPKCS1PrivateKey(privKey)
	privBlock := &pem.Block{
		Type:    "RSA PRIVATE KEY",
		Headers: nil,
		Bytes:   privDER,
	}
	privPEM := string(pem.EncodeToMemory(privBlock))

	pubDER := x509.MarshalPKCS1PublicKey(&privKey.PublicKey)
	verifier := PrivateKeyPrefix + base64.RawURLEncoding.EncodeToString([]byte(privPEM))

	return &ZedKeypair{
		PrivateKey:    privKey,
		PublicKey:     &privKey.PublicKey,
		PrivateKeyPEM: privPEM,
		PublicKeyDER:  pubDER,
		Verifier:      verifier,
	}, nil
}

// EncodeZedPublicKeyVerifier exports RSA public key in PKCS#1 DER format, Base64 URL-safe encode without padding.
func EncodeZedPublicKeyVerifier(pubKey *rsa.PublicKey) string {
	if pubKey == nil {
		return ""
	}

	der := x509.MarshalPKCS1PublicKey(pubKey)

	return base64.RawURLEncoding.EncodeToString(der)
}

func parseRSAFromPEM(pemStr string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("failed to parse PEM block containing private key")
	}

	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err == nil {
		return privKey, nil
	}

	pkcs8Key, errPkcs8 := x509.ParsePKCS8PrivateKey(block.Bytes)
	if errPkcs8 != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	rsaKey, ok := pkcs8Key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("parsed key is not an RSA private key")
	}

	return rsaKey, nil
}

func decodeEncryptedBytes(raw string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err == nil {
		return b, nil
	}

	b, err = base64.URLEncoding.DecodeString(raw)
	if err == nil {
		return b, nil
	}

	b, err = base64.StdEncoding.DecodeString(raw)
	if err == nil {
		return b, nil
	}

	return nil, fmt.Errorf("failed to decode encrypted token base64: %w", err)
}

func unwrapPrivateKeyPEM(pemStr string) (string, error) {
	if !strings.HasPrefix(pemStr, PrivateKeyPrefix) {
		return pemStr, nil
	}

	encoded := strings.TrimPrefix(pemStr, PrivateKeyPrefix)

	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err == nil {
		return string(decoded), nil
	}

	decoded, padErr := base64.URLEncoding.DecodeString(encoded)
	if padErr != nil {
		return "", fmt.Errorf("invalid private key verifier encoding: %w", err)
	}

	return string(decoded), nil
}

// DecryptZedAccessToken decrypts RSA PKCS1v15 or OAEP-SHA256 encrypted access token received during OAuth/native signin.
func DecryptZedAccessToken(privateKeyPEM, encryptedTokenBase64 string) (string, error) {
	pemStr, err := unwrapPrivateKeyPEM(privateKeyPEM)
	if err != nil {
		return "", err
	}

	privKey, err := parseRSAFromPEM(pemStr)
	if err != nil {
		return "", err
	}

	encryptedBytes, err := decodeEncryptedBytes(strings.TrimSpace(encryptedTokenBase64))
	if err != nil {
		return "", err
	}

	decrypted, errOAEP := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, encryptedBytes, nil)
	if errOAEP == nil {
		return string(decrypted), nil
	}

	decrypted, errPkcs1 := rsa.DecryptPKCS1v15(rand.Reader, privKey, encryptedBytes)
	if errPkcs1 == nil {
		return string(decrypted), nil
	}

	return "", fmt.Errorf("failed to decrypt zed access token (oaep: %v, pkcs1v15: %w)", errOAEP, errPkcs1)
}

// BuildZedUserAuthHeader formats Bearer <token> or Zed user auth headers (e.g. "<userId> <accessToken>" or "Bearer <token>").
func BuildZedUserAuthHeader(accessToken string) string {
	trimmed := strings.TrimSpace(accessToken)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "Bearer ") || strings.Contains(trimmed, " ") {
		return trimmed
	}

	return "Bearer " + trimmed
}

type llmTokenRequest struct {
	ClientID string `json:"client_id,omitempty"`
}

type llmTokenResponse struct {
	Token any `json:"token"`
}

func extractTokenValue(token any) (string, bool) {
	if s, ok := token.(string); ok && s != "" {
		return s, true
	}

	if m, ok := token.(map[string]any); ok {
		if val, okVal := m["value"].(string); okVal && val != "" {
			return val, true
		}
	}

	if arr, ok := token.([]any); ok && len(arr) > 0 {
		if s, okItem := arr[0].(string); okItem && s != "" {
			return s, true
		}
	}

	return "", false
}

func parseTokenResponse(respBytes []byte) (string, error) {
	var parsed llmTokenResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse zed response: %w", err)
	}

	if tok, ok := extractTokenValue(parsed.Token); ok {
		return tok, nil
	}

	return "", errors.New("zed did not return a valid LLM token")
}

func createLLMTokenRequest(ctx context.Context, accessToken, clientID string) (*http.Request, error) {
	reqBody := llmTokenRequest{ClientID: clientID}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, DefaultLLMTokenURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", BuildZedUserAuthHeader(accessToken))
	req.Header.Set(ZedHeaders["clientSupportsStatus"], "1")
	req.Header.Set(ZedHeaders["clientSupportsStreamEnded"], "1")
	req.Header.Set(ZedHeaders["clientSupportsXai"], "1")

	return req, nil
}

// FetchZedLLMToken requests a temporary LLM token from Zed Cloud (POST https://cloud.zed.dev/client/llm_tokens).
func FetchZedLLMToken(ctx context.Context, accessToken, clientID string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("accessToken is required")
	}

	req, err := createLLMTokenRequest(ctx, accessToken, clientID)
	if err != nil {
		return "", err
	}

	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute zed llm token request: %w", err)
	}

	if resp == nil || resp.Body == nil {
		return "", fmt.Errorf("empty response received from zed server")
	}

	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[zedauth] close response body: %v", closeErr)
		}
	}()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read zed response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("zed llm token request failed with status %d: %s", resp.StatusCode, string(respBytes))
	}

	return parseTokenResponse(respBytes)
}
