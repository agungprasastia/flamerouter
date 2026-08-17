// Package formats provides message structure adapters and normalization for AI model protocols.
package formats

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	claudeVersion = "2.1.92"
	ccEntrypoint  = "sdk-cli"
)

func generateBillingHeader(payload any) string {
	content, err := json.Marshal(payload)
	if err != nil {
		content = []byte("{}")
	}

	h := sha256.Sum256(content)
	cch := hex.EncodeToString(h[:])[:5]
	b := make([]byte, 2)

	if _, err := rand.Read(b); err != nil {
		b = []byte{0x12, 0x34}
	}

	buildHash := hex.EncodeToString(b)[:3]

	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=%s; cch=%s;",
		claudeVersion, buildHash, ccEntrypoint, cch)
}

func deriveUUID(seed string) string {
	h := sha256.Sum256([]byte(seed))
	hexStr := hex.EncodeToString(h[:])
	nibble := hexStr[16]

	var n int

	if nibble >= '0' && nibble <= '9' {
		n = int(nibble - '0')
	} else {
		n = int(nibble-'a') + 10
	}

	variant := fmt.Sprintf("%x", (n&0x3)|0x8)

	return fmt.Sprintf("%s-%s-4%s-%s%s-%s",
		hexStr[0:8], hexStr[8:12], hexStr[13:16], variant, hexStr[17:20], hexStr[20:32])
}

func generateFakeUserID(sessionID, apiKey string) string {
	var (
		deviceID    string
		accountUUID string
	)

	if apiKey != "" {
		d := sha256.Sum256([]byte("device:" + apiKey))
		deviceID = hex.EncodeToString(d[:])
		accountUUID = deriveUUID("account:" + apiKey)
	} else {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			b = make([]byte, 32)
		}

		deviceID = hex.EncodeToString(b)
		accountUUID = randomUUIDCloak()
	}

	sessionUUID := sessionID
	if sessionUUID == "" {
		sessionUUID = randomUUIDCloak()
	}

	return fmt.Sprintf(`{"device_id":"%s","account_uuid":"%s","session_id":"%s"}`, deviceID, accountUUID, sessionUUID)
}

func randomUUIDCloak() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		b = make([]byte, 16)
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func injectSystemBilling(systemVal any, billingBlock map[string]any) []any {
	switch s := systemVal.(type) {
	case []any:
		if len(s) == 0 {
			return []any{billingBlock}
		}

		if first, ok := s[0].(map[string]any); ok {
			if t, ok := first["text"].(string); ok && strings.HasPrefix(t, "x-anthropic-billing-header:") {
				return s
			}
		}

		return append([]any{billingBlock}, s...)
	case string:
		return []any{billingBlock, map[string]any{"type": "text", "text": s}}
	default:
		return []any{billingBlock}
	}
}

// ApplyCloaking injects billing header + fake user_id for OAuth tokens (sk-ant-oat).
func ApplyCloaking(body map[string]any, apiKey, sessionID string) map[string]any {
	if body == nil || apiKey == "" || !strings.Contains(apiKey, "sk-ant-oat") {
		return body
	}

	billingText := generateBillingHeader(body)
	billingBlock := map[string]any{"type": "text", "text": billingText}

	body["system"] = injectSystemBilling(body["system"], billingBlock)

	meta, ok := body["metadata"].(map[string]any)
	if !ok || meta == nil {
		meta = map[string]any{}
	}

	if _, has := meta["user_id"]; !has {
		meta["user_id"] = generateFakeUserID(sessionID, apiKey)
		body["metadata"] = meta
	}

	return body
}
