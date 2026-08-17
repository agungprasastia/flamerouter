package utils

import (
	"encoding/json"
	"strings"
)

// SKIP_PATTERNS: requests containing these texts bypass the provider.
// Parity with 9router open-sse/config/runtimeConfig.js SKIP_PATTERNS.
var skipPatterns = []string{
	"Please write a 5-10 word title for the following conversation:",
}

func checkLastMessageAssistantJSON(last map[string]any) bool {
	role, ok := last["role"].(string)
	if !ok || role != "assistant" {
		return false
	}

	if arr, ok := last["content"].([]any); ok && len(arr) > 0 {
		if block, ok := arr[0].(map[string]any); ok {
			if t, ok := block["text"].(string); ok && t == "{" {
				return true
			}
		}
	}

	return messageText(last) == "{"
}

func hasSkipPattern(text string) bool {
	for _, p := range skipPatterns {
		if strings.Contains(text, p) {
			return true
		}
	}

	return false
}

func checkUserMessagesForBypass(msgs []any) bool {
	if len(msgs) == 1 {
		if msg, ok := msgs[0].(map[string]any); ok {
			if role, ok := msg["role"].(string); ok && role == "user" && messageText(msg) == "count" {
				return true
			}
		}
	}

	return checkUserMessagesContent(msgs)
}

func checkUserMessagesContent(msgs []any) bool {
	var userParts []string

	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		if role, ok := msg["role"].(string); ok && role == "user" {
			userParts = append(userParts, messageText(msg))
		}
	}

	return hasSkipPattern(strings.Join(userParts, " "))
}

func checkBypassSignals(req map[string]any, msgs []any) bool {
	if last, ok := msgs[len(msgs)-1].(map[string]any); ok && checkLastMessageAssistantJSON(last) {
		return true
	}

	if first, ok := msgs[0].(map[string]any); ok && messageText(first) == "Warmup" {
		return true
	}

	if checkUserMessagesForBypass(msgs) {
		return true
	}

	systemText := collectSystemText(req, msgs)

	return strings.Contains(systemText, "isNewTopic")
}

// ShouldBypass returns true for Claude naming/warmup requests that waste quota.
// Only applies when client is "claude-code" or "claude" (9router: UA has claude-cli).
func ShouldBypass(body []byte, client string) bool {
	if len(body) == 0 || (client != "claude-code" && client != "claude") {
		return false
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}

	msgs, ok := req["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return false
	}

	return checkBypassSignals(req, msgs)
}

func extractTextFromBlocks(blocks []any) string {
	var b strings.Builder

	for _, item := range blocks {
		block, ok := item.(map[string]any)
		if !ok || block == nil {
			continue
		}

		t, _ := block["type"].(string) //nolint:errcheck // type fallback string
		if t == "text" || t == "" {
			if text, ok := block["text"].(string); ok && text != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}

				b.WriteString(text)
			}
		}
	}

	return b.String()
}

func messageText(msg map[string]any) string {
	if msg == nil {
		return ""
	}

	switch c := msg["content"].(type) {
	case string:
		return c
	case []any:
		return extractTextFromBlocks(c)
	}

	return ""
}

func collectSystemFromBlocks(s []any) []string {
	var parts []string

	for _, item := range s {
		block, ok := item.(map[string]any)
		if !ok || block == nil {
			continue
		}

		t, _ := block["type"].(string) //nolint:errcheck // type fallback string
		if t == "text" || t == "" {
			if text, ok := block["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
	}

	return parts
}

func collectSystemText(req map[string]any, msgs []any) string {
	var parts []string

	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		if role, ok := msg["role"].(string); ok && role == "system" {
			parts = append(parts, messageText(msg))
		}
	}

	switch s := req["system"].(type) {
	case string:
		parts = append(parts, s)
	case []any:
		parts = append(parts, collectSystemFromBlocks(s)...)
	}

	return strings.Join(parts, " ")
}
