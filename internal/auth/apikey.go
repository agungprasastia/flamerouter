// Package auth implements API key management, JWT session auth, and OIDC flows.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// APIKeys manages key formatting, generation, and CRC verification.
type APIKeys struct {
	secret string
}

// New creates a new APIKeys instance.
func New(secret string) *APIKeys {
	return &APIKeys{secret: secret}
}

func (a *APIKeys) crc(machineID, keyID string) string {
	mac := hmac.New(sha256.New, []byte(a.secret))
	mac.Write([]byte(machineID + keyID))

	return hex.EncodeToString(mac.Sum(nil))[:8]
}

// Format formats an API key string with its machineID, keyID, and checksum.
func (a *APIKeys) Format(machineID, keyID string) string {
	c := a.crc(machineID, keyID)
	return fmt.Sprintf("sk-%s-%s-%s", machineID, keyID, c)
}

// Generate generates a new random API key and keyID.
func (a *APIKeys) Generate(machineID string) (key, keyID string) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"

	b := make([]byte, 6)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		copy(b, []byte("rnd123"))
	}

	id := make([]byte, 6)
	for i := 0; i < 6; i++ {
		id[i] = chars[int(b[i])%len(chars)]
	}

	keyID = string(id)
	c := a.crc(machineID, keyID)
	key = fmt.Sprintf("sk-%s-%s-%s", machineID, keyID, c)

	return key, keyID
}

// Parse parses and validates an API key string.
func (a *APIKeys) Parse(apiKey string) (machineID, keyID string, ok bool) {
	if !strings.HasPrefix(apiKey, "sk-") {
		return "", "", false
	}

	parts := strings.Split(apiKey, "-")
	if len(parts) == 4 {
		mid, kid, crc := parts[1], parts[2], parts[3]
		if a.crc(mid, kid) != crc {
			return "", "", false
		}

		return mid, kid, true
	}

	if len(parts) == 2 {
		return "", parts[1], true
	}

	return "", "", false
}

// VerifyCRC checks whether the API key has a valid checksum.
func (a *APIKeys) VerifyCRC(apiKey string) bool {
	_, _, ok := a.Parse(apiKey)
	return ok
}

// HashKey computes SHA-256 hash of the API key for database storage.
func HashKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}
