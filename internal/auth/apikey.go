package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type APIKeys struct {
	secret string
}

func New(secret string) *APIKeys {
	return &APIKeys{secret: secret}
}

func (a *APIKeys) crc(machineID, keyID string) string {
	mac := hmac.New(sha256.New, []byte(a.secret))
	mac.Write([]byte(machineID + keyID))

	return hex.EncodeToString(mac.Sum(nil))[:8]
}

func (a *APIKeys) Format(machineID, keyID string) string {
	c := a.crc(machineID, keyID)
	return fmt.Sprintf("sk-%s-%s-%s", machineID, keyID, c)
}

func (a *APIKeys) Generate(machineID string) (key, keyID string) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"

	b := make([]byte, 6)
	_, _ = rand.Read(b)

	id := make([]byte, 6)
	for i := 0; i < 6; i++ {
		id[i] = chars[int(b[i])%len(chars)]
	}

	keyID = string(id)
	c := a.crc(machineID, keyID)
	key = fmt.Sprintf("sk-%s-%s-%s", machineID, keyID, c)

	return key, keyID
}

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

func (a *APIKeys) VerifyCRC(apiKey string) bool {
	_, _, ok := a.Parse(apiKey)
	return ok
}

func HashKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}
