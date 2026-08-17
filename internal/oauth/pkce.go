// Package oauth provides OAuth authentication flows, token lifecycle helpers,
// and specialized third-party login providers.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// GenerateCodeVerifier generates a 32-byte cryptographic random URL-safe PKCE verifier.
func GenerateCodeVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// GenerateCodeChallenge creates a SHA-256 base64url challenge from a PKCE verifier.
func GenerateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(h[:])
}

// GenerateState generates a 16-byte cryptographic random URL-safe OAuth state parameter.
func GenerateState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
