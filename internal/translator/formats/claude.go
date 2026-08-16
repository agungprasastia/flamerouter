package formats

import (
	"encoding/base64"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"regexp"
	"strings"
)

const (
	maxClaudeSignatureLen = 32 * 1024 * 1024
	claudeSignatureMarker = 0x12
)

var adaptiveThinkingUnsupported = regexp.MustCompile(`(?i)haiku`)

func stripCachePrefix(raw string) string {
	sig := strings.TrimSpace(raw)
	if sig == "" {
		return ""
	}

	if idx := strings.Index(sig, "#"); idx >= 0 {
		return strings.TrimSpace(sig[idx+1:])
	}

	return sig
}

// IsValidClaudeSignature validates Claude thinking signature (E/R form).
func IsValidClaudeSignature(rawSignature string) bool {
	sig := stripCachePrefix(rawSignature)
	if sig == "" || len(sig) > maxClaudeSignatureLen {
		return false
	}

	if sig[0] == 'E' {
		decoded, err := base64.StdEncoding.DecodeString(sig)
		if err != nil || len(decoded) == 0 {
			// try raw std with padding issues
			decoded, err = base64.RawStdEncoding.DecodeString(sig)
			if err != nil || len(decoded) == 0 {
				return false
			}
		}

		return decoded[0] == claudeSignatureMarker
	}

	if sig[0] == 'R' {
		outer, err := base64.StdEncoding.DecodeString(sig)
		if err != nil || len(outer) == 0 {
			outer, err = base64.RawStdEncoding.DecodeString(sig)
			if err != nil || len(outer) == 0 {
				return false
			}
		}

		if outer[0] != 0x45 { // 'E'
			return false
		}

		inner, err := base64.StdEncoding.DecodeString(string(outer))
		if err != nil || len(inner) == 0 {
			inner, err = base64.RawStdEncoding.DecodeString(string(outer))
			if err != nil || len(inner) == 0 {
				return false
			}
		}

		return inner[0] == claudeSignatureMarker
	}

	return false
}

func buildThinkingPlaceholder(provider string) map[string]any {
	block := map[string]any{
		"type":     schema.ClaudeBlockThinking,
		"thinking": ".",
	}
	if provider != "deepseek" {
		block["signature"] = DefaultThinkingClaudeSignature
	}

	return block
}

// NormalizeClaudePassthrough matches 9router formats/claude.js normalizeClaudePassthrough.
func NormalizeClaudePassthrough(body map[string]any, model string) map[string]any {
	if body == nil {
		return body
	}

	if thinking, ok := body["thinking"].(map[string]any); ok {
		if t, _ := thinking["type"].(string); t == "adaptive" && adaptiveThinkingUnsupported.MatchString(model) {
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 10000}
		}
	}

	if adaptiveThinkingUnsupported.MatchString(model) {
		if oc, ok := body["output_config"].(map[string]any); ok {
			if _, has := oc["effort"]; has {
				delete(oc, "effort")

				if len(oc) == 0 {
					delete(body, "output_config")
				}
			}
		}
	}

	if messages, ok := body["messages"].([]any); ok {
		var systemBlocks []any

		var kept []any

		for _, msgRaw := range messages {
			msg, ok := msgRaw.(map[string]any)
			if !ok {
				kept = append(kept, msgRaw)
				continue
			}

			role, _ := msg["role"].(string)
			if role == schema.RoleSystem {
				text := ""
				switch c := msg["content"].(type) {
				case string:
					text = c
				case []any:
					var parts []string

					for _, b := range c {
						if s, ok := b.(string); ok {
							parts = append(parts, s)
						} else if m, ok := b.(map[string]any); ok {
							if t, ok := m["text"].(string); ok {
								parts = append(parts, t)
							}
						}
					}

					text = strings.Join(parts, "\n")
				}

				if strings.TrimSpace(text) != "" {
					systemBlocks = append(systemBlocks, map[string]any{"type": schema.ClaudeBlockText, "text": text})
				}

				continue
			}

			kept = append(kept, msg)
		}

		if len(systemBlocks) > 0 {
			var existing []any
			switch s := body["system"].(type) {
			case []any:
				existing = s
			case string:
				if strings.TrimSpace(s) != "" {
					existing = []any{map[string]any{"type": "text", "text": s}}
				}
			}

			body["system"] = append(existing, systemBlocks...)
			body["messages"] = kept
			messages = kept
		}

		thinkingEnabled := false

		if t, ok := body["thinking"].(map[string]any); ok {
			if tt, _ := t["type"].(string); tt == "enabled" {
				thinkingEnabled = true
			}
		}

		for _, msgRaw := range messages {
			msg, ok := msgRaw.(map[string]any)
			if !ok {
				continue
			}

			role, _ := msg["role"].(string)
			if role != schema.RoleAssistant {
				continue
			}

			content, ok := msg["content"].([]any)
			if !ok {
				continue
			}

			hasToolUse := false
			hasKeptThinking := false

			var keptBlocks []any

			for _, blockRaw := range content {
				block, ok := blockRaw.(map[string]any)
				if !ok {
					keptBlocks = append(keptBlocks, blockRaw)
					continue
				}

				bt, _ := block["type"].(string)
				if bt == schema.ClaudeBlockThinking || bt == schema.ClaudeBlockRedactedThinking {
					sig, _ := block["signature"].(string)
					if IsValidClaudeSignature(sig) {
						hasKeptThinking = true

						keptBlocks = append(keptBlocks, block)
					}

					continue
				}

				if bt == schema.ClaudeBlockToolUse {
					hasToolUse = true
				}

				keptBlocks = append(keptBlocks, block)
			}

			msg["content"] = keptBlocks
			if thinkingEnabled && !hasKeptThinking && hasToolUse {
				msg["content"] = append([]any{buildThinkingPlaceholder("claude")}, keptBlocks...)
			}
		}
	}

	return body
}

// HasValidContent matches 9router hasValidContent.
func HasValidContent(msg map[string]any) bool {
	switch c := msg["content"].(type) {
	case string:
		return strings.TrimSpace(c) != ""
	case []any:
		for _, blockRaw := range c {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				continue
			}

			bt, _ := block["type"].(string)
			if bt == schema.ClaudeBlockText {
				if t, ok := block["text"].(string); ok && strings.TrimSpace(t) != "" {
					return true
				}
			}

			if bt == schema.ClaudeBlockToolUse || bt == schema.ClaudeBlockToolResult {
				return true
			}
		}
	}

	return false
}

// FixToolUseOrdering matches 9router fixToolUseOrdering.
func FixToolUseOrdering(messages []any) []any {
	if len(messages) <= 1 {
		return messages
	}

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)
		content, ok := msg["content"].([]any)

		if !ok || role != schema.RoleAssistant {
			continue
		}

		hasToolUse := false

		for _, b := range content {
			if block, ok := b.(map[string]any); ok {
				if t, _ := block["type"].(string); t == schema.ClaudeBlockToolUse {
					hasToolUse = true
					break
				}
			}
		}

		if !hasToolUse {
			continue
		}

		var newContent []any

		foundToolUse := false

		for _, b := range content {
			block, ok := b.(map[string]any)
			if !ok {
				newContent = append(newContent, b)
				continue
			}

			bt, _ := block["type"].(string)
			if bt == schema.ClaudeBlockToolUse {
				foundToolUse = true

				newContent = append(newContent, block)
			} else if bt == schema.ClaudeBlockThinking || bt == schema.ClaudeBlockRedactedThinking {
				newContent = append(newContent, block)
			} else if !foundToolUse {
				newContent = append(newContent, block)
			}
		}

		msg["content"] = newContent
	}

	var merged []any

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			merged = append(merged, msgRaw)
			continue
		}

		if len(merged) > 0 {
			last, ok := merged[len(merged)-1].(map[string]any)
			if ok {
				lastRole, _ := last["role"].(string)
				role, _ := msg["role"].(string)

				if lastRole == role {
					lastContent := toContentArr(last["content"])
					msgContent := toContentArr(msg["content"])

					var toolResults, other []any

					for _, b := range append(lastContent, msgContent...) {
						if block, ok := b.(map[string]any); ok {
							if t, _ := block["type"].(string); t == schema.ClaudeBlockToolResult {
								toolResults = append(toolResults, b)
								continue
							}
						}

						other = append(other, b)
					}

					last["content"] = append(toolResults, other...)

					continue
				}
			}
		}

		content := toContentArr(msg["content"])
		merged = append(merged, map[string]any{"role": msg["role"], "content": append([]any{}, content...)})
	}

	return merged
}

func toContentArr(c any) []any {
	if arr, ok := c.([]any); ok {
		return arr
	}

	if s, ok := c.(string); ok {
		return []any{map[string]any{"type": schema.ClaudeBlockText, "text": s}}
	}

	return nil
}

// AdjustMaxTokens re-export for convenience at formats layer.
func AdjustMaxTokens(body map[string]any, ceiling int) int {
	return concerns.AdjustMaxTokens(body, ceiling)
}
