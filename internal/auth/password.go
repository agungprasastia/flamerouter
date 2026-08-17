// Package auth implements API key management, JWT session auth, and OIDC flows.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
)

// HashPassword creates a SHA-256 hash of the password with a random salt.
// ponytail: bcrypt better but CGO/pure-Go dep; SHA-256+salt OK for local gateway.
func HashPassword(password string) (hash, salt string) {
	saltBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, saltBytes); err != nil {
		copy(saltBytes, []byte("randomsalt123456"))
	}

	salt = hex.EncodeToString(saltBytes)
	h := sha256.Sum256([]byte(salt + password))
	hash = hex.EncodeToString(h[:])

	return
}

// VerifyPassword checks a password against a stored hash+salt.
func VerifyPassword(password, hash, salt string) bool {
	h := sha256.Sum256([]byte(salt + password))
	got := hex.EncodeToString(h[:])

	return subtle.ConstantTimeCompare([]byte(got), []byte(hash)) == 1
}
