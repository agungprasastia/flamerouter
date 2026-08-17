// Package formats provides message structure adapters and normalization for AI model protocols.
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

func decodeBase64WithFallback(sig string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(sig)
	if err != nil || len(decoded) == 0 {
		decoded, err = base64.RawStdEncoding.DecodeString(sig)
	}

	return decoded, err
}

func checkSigE(sig string) bool {
	decoded, err := decodeBase64WithFallback(sig)
	if err != nil || len(decoded) == 0 {
		return false
	}

	return decoded[0] == claudeSignatureMarker
}

func checkSigR(sig string) bool {
	outer, err := decodeBase64WithFallback(sig)
	if err != nil || len(outer) == 0 || outer[0] != 0x45 { // 'E'
		return false
	}

	inner, err := decodeBase64WithFallback(string(outer))
	if err != nil || len(inner) == 0 {
		return false
	}

	return inner[0] == claudeSignatureMarker
}

// IsValidClaudeSignature validates Claude thinking signature (E/R form).
func IsValidClaudeSignature(rawSignature string) bool {
	sig := stripCachePrefix(rawSignature)
	if sig == "" || len(sig) > maxClaudeSignatureLen {
		return false
	}

	if sig[0] == 'E' {
		return checkSigE(sig)
	}

	if sig[0] == 'R' {
		return checkSigR(sig)
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

func normalizeAdaptiveEffort(body map[string]any, model string) {
	if !adaptiveThinkingUnsupported.MatchString(model) {
		return
	}

	oc, ok := body["output_config"].(map[string]any)
	if !ok {
		return
	}

	if _, has := oc["effort"]; has {
		delete(oc, "effort")

		if len(oc) == 0 {
			delete(body, "output_config")
		}
	}
}

func normalizeThinkingConfig(body map[string]any, model string) {
	if thinking, ok := body["thinking"].(map[string]any); ok {
		if t, ok := thinking["type"].(string); ok && t == "adaptive" && adaptiveThinkingUnsupported.MatchString(model) {
			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": 10000}
		}
	}

	normalizeAdaptiveEffort(body, model)
}

func extractSystemText(c any) string {
	switch content := c.(type) {
	case string:
		return content
	case []any:
		var parts []string

		for _, b := range content {
			if s, ok := b.(string); ok {
				parts = append(parts, s)
			} else if m, ok := b.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}

		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func extractSystemBlocks(messages []any) ([]any, []any) {
	var systemBlocks []any

	kept := make([]any, 0, len(messages))

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			kept = append(kept, msgRaw)
			continue
		}

		role, ok := msg["role"].(string)
		if ok && role == schema.RoleSystem {
			text := extractSystemText(msg["content"])
			if strings.TrimSpace(text) != "" {
				systemBlocks = append(systemBlocks, map[string]any{"type": schema.ClaudeBlockText, "text": text})
			}

			continue
		}

		kept = append(kept, msg)
	}

	return systemBlocks, kept
}

func appendSystemBlocks(body map[string]any, systemBlocks []any) {
	if len(systemBlocks) == 0 {
		return
	}

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
}

func filterAssistantBlocks(content []any) ([]any, bool, bool) {
	keptBlocks := make([]any, 0, len(content))
	hasToolUse := false
	hasKeptThinking := false

	for _, blockRaw := range content {
		block, ok := blockRaw.(map[string]any)
		if !ok {
			keptBlocks = append(keptBlocks, blockRaw)
			continue
		}

		bt, ok := block["type"].(string)
		if !ok {
			keptBlocks = append(keptBlocks, block)
			continue
		}

		if bt == schema.ClaudeBlockThinking || bt == schema.ClaudeBlockRedactedThinking {
			if sig, ok := block["signature"].(string); ok && IsValidClaudeSignature(sig) {
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

	return keptBlocks, hasToolUse, hasKeptThinking
}

func normalizeAssistantMessages(messages []any, thinkingEnabled bool) {
	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok || role != schema.RoleAssistant {
			continue
		}

		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		keptBlocks, hasToolUse, hasKeptThinking := filterAssistantBlocks(content)
		msg["content"] = keptBlocks

		if thinkingEnabled && !hasKeptThinking && hasToolUse {
			msg["content"] = append([]any{buildThinkingPlaceholder("claude")}, keptBlocks...)
		}
	}
}

// NormalizeClaudePassthrough matches 9router formats/claude.js normalizeClaudePassthrough.
func NormalizeClaudePassthrough(body map[string]any, model string) map[string]any {
	if body == nil {
		return body
	}

	normalizeThinkingConfig(body, model)

	messages, ok := body["messages"].([]any)
	if !ok {
		return body
	}

	systemBlocks, kept := extractSystemBlocks(messages)
	if len(systemBlocks) > 0 {
		appendSystemBlocks(body, systemBlocks)
		body["messages"] = kept
		messages = kept
	}

	thinkingEnabled := false
	if t, ok := body["thinking"].(map[string]any); ok {
		thinkingEnabled = t["type"] == "enabled"
	}

	normalizeAssistantMessages(messages, thinkingEnabled)

	return body
}

func hasValidBlock(block map[string]any) bool {
	bt, ok := block["type"].(string)
	if !ok {
		return false
	}

	if bt == schema.ClaudeBlockText {
		if t, ok := block["text"].(string); ok && strings.TrimSpace(t) != "" {
			return true
		}
	}

	return bt == schema.ClaudeBlockToolUse || bt == schema.ClaudeBlockToolResult
}

// HasValidContent matches 9router hasValidContent.
func HasValidContent(msg map[string]any) bool {
	switch c := msg["content"].(type) {
	case string:
		return strings.TrimSpace(c) != ""
	case []any:
		for _, blockRaw := range c {
			if block, ok := blockRaw.(map[string]any); ok && hasValidBlock(block) {
				return true
			}
		}
	}

	return false
}

func filterAssistantToolUse(content []any) []any {
	var (
		newContent   []any
		foundToolUse bool
	)

	for _, b := range content {
		block, ok := b.(map[string]any)
		if !ok {
			newContent = append(newContent, b)
			continue
		}

		bt, ok := block["type"].(string)
		if !ok {
			newContent = append(newContent, block)
			continue
		}

		switch {
		case bt == schema.ClaudeBlockToolUse:
			foundToolUse = true

			newContent = append(newContent, block)
		case bt == schema.ClaudeBlockThinking || bt == schema.ClaudeBlockRedactedThinking:
			newContent = append(newContent, block)
		case !foundToolUse:
			newContent = append(newContent, block)
		}
	}

	return newContent
}

func hasToolUseBlock(content []any) bool {
	for _, b := range content {
		if block, ok := b.(map[string]any); ok {
			if t, ok := block["type"].(string); ok && t == schema.ClaudeBlockToolUse {
				return true
			}
		}
	}

	return false
}

func fixAssistantToolUseOrdering(messages []any) {
	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok || role != schema.RoleAssistant {
			continue
		}

		content, ok := msg["content"].([]any)
		if !ok || !hasToolUseBlock(content) {
			continue
		}

		msg["content"] = filterAssistantToolUse(content)
	}
}

func tryMergeWithPrevious(merged []any, msg map[string]any) bool {
	if len(merged) == 0 {
		return false
	}

	last, ok := merged[len(merged)-1].(map[string]any)
	if !ok {
		return false
	}

	lastRole, okLast := last["role"].(string)
	if !okLast {
		return false
	}

	role, okRole := msg["role"].(string)
	if !okRole || lastRole != role {
		return false
	}

	lastContent := toContentArr(last["content"])
	msgContent := toContentArr(msg["content"])
	allBlocks := make([]any, 0, len(lastContent)+len(msgContent))
	allBlocks = append(allBlocks, lastContent...)
	allBlocks = append(allBlocks, msgContent...)

	toolResults := make([]any, 0, len(allBlocks))
	other := make([]any, 0, len(allBlocks))

	for _, b := range allBlocks {
		if block, ok := b.(map[string]any); ok {
			if t, ok := block["type"].(string); ok && t == schema.ClaudeBlockToolResult {
				toolResults = append(toolResults, b)
				continue
			}
		}

		other = append(other, b)
	}

	last["content"] = append(toolResults, other...)

	return true
}

func mergeConsecutiveRoles(messages []any) []any {
	merged := make([]any, 0, len(messages))

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			merged = append(merged, msgRaw)
			continue
		}

		if tryMergeWithPrevious(merged, msg) {
			continue
		}

		content := toContentArr(msg["content"])
		merged = append(merged, map[string]any{"role": msg["role"], "content": append([]any{}, content...)})
	}

	return merged
}

// FixToolUseOrdering matches 9router fixToolUseOrdering.
func FixToolUseOrdering(messages []any) []any {
	if len(messages) <= 1 {
		return messages
	}

	fixAssistantToolUseOrdering(messages)

	return mergeConsecutiveRoles(messages)
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
