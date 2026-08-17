package executor

import (
	"time"

	"github.com/google/uuid"
)

// GenerateCursorSessionID generates a deterministic session ID using UUID v5 with DNS namespace.
func GenerateCursorSessionID(authToken string) string {
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(authToken)).String()
}

// GenerateCursorChecksum generates the checksum header for Cursor API requests (Jyh Cipher).
// Algorithm:
// 1. Get Unix timestamp in ms / 1e6 (equivalent to Date.now() / 1e6).
// 2. Build 6-byte big-endian array from timestamp.
// 3. Jyh cipher: XOR each byte with t (starts 165), update t = byte.
// 4. URL-safe base64 encode without padding.
// 5. Returns {base64}{machineId}.
func GenerateCursorChecksum(machineID string) string {
	timestamp := time.Now().UnixMilli() / 1000

	byteArray := []byte{
		byte((timestamp >> 40) & 0xFF),
		byte((timestamp >> 32) & 0xFF),
		byte((timestamp >> 24) & 0xFF),
		byte((timestamp >> 16) & 0xFF),
		byte((timestamp >> 8) & 0xFF),
		byte(timestamp & 0xFF),
	}

	t := byte(165)
	for i := 0; i < len(byteArray); i++ {
		byteArray[i] = ((byteArray[i] ^ t) + byte(i%256)) & 0xFF
		t = byteArray[i]
	}

	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"

	var (
		encoded []byte
		b, c    byte
	)

	for i := 0; i < len(byteArray); i += 3 {
		a := byteArray[i]
		b = 0
		c = 0

		if i+1 < len(byteArray) {
			b = byteArray[i+1]
		}

		if i+2 < len(byteArray) {
			c = byteArray[i+2]
		}

		encoded = append(encoded, alphabet[a>>2])
		encoded = append(encoded, alphabet[((a&3)<<4)|(b>>4)])

		if i+1 < len(byteArray) {
			encoded = append(encoded, alphabet[((b&15)<<2)|(c>>6)])
		}

		if i+2 < len(byteArray) {
			encoded = append(encoded, alphabet[c&63])
		}
	}

	return string(encoded) + machineID
}
