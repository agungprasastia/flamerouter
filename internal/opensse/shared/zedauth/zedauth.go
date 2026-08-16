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
	"net/http"
	"strings"
	"time"
)

const (
	PrivateKeyPrefix   = "zed-rsa-pkcs1:"
	ZedWebBaseURL      = "https://zed.dev"
	ZedCloudBaseURL    = "https://cloud.zed.dev"
	ZedLLMBaseURL      = "https://cloud.zed.dev"
	DefaultLLMTokenURL = "https://cloud.zed.dev/client/llm_tokens"
)

// ZED_HEADERS map containing standard Zed header keys.
var ZED_HEADERS = map[string]string{
	"expiredToken":              "x-zed-expired-token",
	"outdatedToken":             "x-zed-outdated-token",
	"clientSupportsStatus":      "x-zed-client-supports-status-messages",
	"clientSupportsStreamEnded": "x-zed-client-supports-stream-ended-request-completion-status",
	"serverSupportsStatus":      "x-zed-server-supports-status-messages",
	"clientSupportsXai":         "x-zed-client-supports-x-ai",
	"systemId":                  "x-zed-system-id",
}

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
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
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

// DecryptZedAccessToken decrypts RSA PKCS1v15 or OAEP-SHA256 encrypted access token received during OAuth/native signin.
func DecryptZedAccessToken(privateKeyPEM, encryptedTokenBase64 string) (string, error) {
	pemStr := privateKeyPEM
	if strings.HasPrefix(pemStr, PrivateKeyPrefix) {
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(pemStr, PrivateKeyPrefix))
		if err != nil {
			// ponytail: try padded base64url if unpadded fails
			var padErr error

			decoded, padErr = base64.URLEncoding.DecodeString(strings.TrimPrefix(pemStr, PrivateKeyPrefix))
			if padErr != nil {
				return "", fmt.Errorf("invalid private key verifier encoding: %w", err)
			}
		}

		pemStr = string(decoded)
	}

	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return "", errors.New("failed to parse PEM block containing private key")
	}

	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Attempt PKCS8 fallback if PKCS1 parse fails
		pkcs8Key, errPkcs8 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if errPkcs8 != nil {
			return "", fmt.Errorf("failed to parse private key: %w", err)
		}

		var ok bool

		privKey, ok = pkcs8Key.(*rsa.PrivateKey)
		if !ok {
			return "", errors.New("parsed key is not an RSA private key")
		}
	}

	rawEncrypted := strings.TrimSpace(encryptedTokenBase64)
	encryptedBytes, err := base64.RawURLEncoding.DecodeString(rawEncrypted)
	if err != nil {
		encryptedBytes, err = base64.URLEncoding.DecodeString(rawEncrypted)
		if err != nil {
			encryptedBytes, err = base64.StdEncoding.DecodeString(rawEncrypted)
			if err != nil {
				return "", fmt.Errorf("failed to decode encrypted token base64: %w", err)
			}
		}
	}

	// Try OAEP SHA-256 first (Zed JS default)
	decrypted, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privKey, encryptedBytes, nil)
	if err == nil {
		return string(decrypted), nil
	}

	// Fallback to PKCS1v15 decryption
	decrypted, errPkcs1 := rsa.DecryptPKCS1v15(rand.Reader, privKey, encryptedBytes)
	if errPkcs1 == nil {
		return string(decrypted), nil
	}

	return "", fmt.Errorf("failed to decrypt zed access token (oaep: %v, pkcs1v15: %w)", err, errPkcs1)
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

// FetchZedLLMToken requests a temporary LLM token from Zed Cloud (POST https://cloud.zed.dev/client/llm_tokens).
func FetchZedLLMToken(ctx context.Context, accessToken, clientID string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("accessToken is required")
	}

	reqBody := llmTokenRequest{ClientID: clientID}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, DefaultLLMTokenURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", BuildZedUserAuthHeader(accessToken))
	req.Header.Set(ZED_HEADERS["clientSupportsStatus"], "1")
	req.Header.Set(ZED_HEADERS["clientSupportsStreamEnded"], "1")
	req.Header.Set(ZED_HEADERS["clientSupportsXai"], "1")

	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute zed llm token request: %w", err)
	}

	if resp == nil || resp.Body == nil {
		return "", fmt.Errorf("empty response received from zed server")
	}

	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read zed response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("zed llm token request failed with status %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed llmTokenResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("failed to parse zed response: %w", err)
	}

	switch v := parsed.Token.(type) {
	case string:
		if v != "" {
			return v, nil
		}
	case map[string]any:
		if val, ok := v["value"].(string); ok && val != "" {
			return val, nil
		}
	case []any:
		if len(v) > 0 {
			if str, ok := v[0].(string); ok && str != "" {
				return str, nil
			}
		}
	}

	return "", errors.New("zed did not return a valid LLM token")
}
