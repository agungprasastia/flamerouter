package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
)

// HashPassword creates a SHA-256 hash of the password with a random salt.
// ponytail: bcrypt better but CGO/pure-Go dep; SHA-256+salt OK for local gateway.
func HashPassword(password string) (hash, salt string) {
	saltBytes := make([]byte, 16)
	_, _ = rand.Read(saltBytes)
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
