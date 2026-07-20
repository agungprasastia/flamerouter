package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// GenerateCursorChecksum generates the checksum header for Cursor API requests.
func GenerateCursorChecksum(machineID string) string {
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	h := sha256.New()
	h.Write([]byte(machineID + ts))
	return hex.EncodeToString(h.Sum(nil))[:32]
}
