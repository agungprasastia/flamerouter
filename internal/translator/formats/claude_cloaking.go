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
	content, _ := json.Marshal(payload)
	h := sha256.Sum256(content)
	cch := hex.EncodeToString(h[:])[:5]
	b := make([]byte, 2)
	rand.Read(b)
	buildHash := hex.EncodeToString(b)[:3]
	return fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=%s; cch=%s;",
		claudeVersion, buildHash, ccEntrypoint, cch)
}

func deriveUuid(seed string) string {
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

func generateFakeUserID(sessionId, apiKey string) string {
	var deviceId, accountUuid string
	if apiKey != "" {
		d := sha256.Sum256([]byte("device:" + apiKey))
		deviceId = hex.EncodeToString(d[:])
		accountUuid = deriveUuid("account:" + apiKey)
	} else {
		b := make([]byte, 32)
		rand.Read(b)
		deviceId = hex.EncodeToString(b)
		accountUuid = randomUUIDCloak()
	}
	sessionUuid := sessionId
	if sessionUuid == "" {
		sessionUuid = randomUUIDCloak()
	}
	return fmt.Sprintf(`{"device_id":"%s","account_uuid":"%s","session_id":"%s"}`, deviceId, accountUuid, sessionUuid)
}

func randomUUIDCloak() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ApplyCloaking injects billing header + fake user_id for OAuth tokens (sk-ant-oat).
func ApplyCloaking(body map[string]any, apiKey, sessionId string) map[string]any {
	if body == nil || apiKey == "" || !strings.Contains(apiKey, "sk-ant-oat") {
		return body
	}

	billingText := generateBillingHeader(body)
	billingBlock := map[string]any{"type": "text", "text": billingText}

	switch s := body["system"].(type) {
	case []any:
		if len(s) > 0 {
			if first, ok := s[0].(map[string]any); ok {
				if t, _ := first["text"].(string); strings.HasPrefix(t, "x-anthropic-billing-header:") {
					// already injected
				} else {
					body["system"] = append([]any{billingBlock}, s...)
				}
			} else {
				body["system"] = append([]any{billingBlock}, s...)
			}
		} else {
			body["system"] = []any{billingBlock}
		}
	case string:
		body["system"] = []any{billingBlock, map[string]any{"type": "text", "text": s}}
	default:
		body["system"] = []any{billingBlock}
	}

	meta, _ := body["metadata"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}
	if _, has := meta["user_id"]; !has {
		meta["user_id"] = generateFakeUserID(sessionId, apiKey)
		body["metadata"] = meta
	}
	return body
}
