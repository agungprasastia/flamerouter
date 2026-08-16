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

// ShouldBypass returns true for Claude naming/warmup requests that waste quota.
// Only applies when client is "claude-code" or "claude" (9router: UA has claude-cli).
func ShouldBypass(body []byte, client string) bool {
	if len(body) == 0 {
		return false
	}

	if client != "claude-code" && client != "claude" {
		return false
	}

	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}

	msgs, _ := req["messages"].([]any)
	if len(msgs) == 0 {
		return false
	}

	// Pattern 1: title extraction (assistant last message text == "{")
	if last, ok := msgs[len(msgs)-1].(map[string]any); ok {
		if role, _ := last["role"].(string); role == "assistant" {
			if arr, ok := last["content"].([]any); ok && len(arr) > 0 {
				if block, ok := arr[0].(map[string]any); ok {
					if t, _ := block["text"].(string); t == "{" {
						return true
					}
				}
			}

			if messageText(last) == "{" {
				return true
			}
		}
	}

	// Pattern 2: Warmup
	if first, ok := msgs[0].(map[string]any); ok {
		if messageText(first) == "Warmup" {
			return true
		}
	}

	// Pattern 3: single "count" user message
	if len(msgs) == 1 {
		if msg, ok := msgs[0].(map[string]any); ok {
			role, _ := msg["role"].(string)
			if role == "user" && messageText(msg) == "count" {
				return true
			}
		}
	}

	// Pattern 4: skip patterns in user text
	var userParts []string

	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		if msg == nil {
			continue
		}

		if role, _ := msg["role"].(string); role == "user" {
			userParts = append(userParts, messageText(msg))
		}
	}

	userText := strings.Join(userParts, " ")
	for _, p := range skipPatterns {
		if strings.Contains(userText, p) {
			return true
		}
	}

	// Pattern 5: CC naming (isNewTopic in system)
	systemText := collectSystemText(req, msgs)
	return strings.Contains(systemText, "isNewTopic")
}

func messageText(msg map[string]any) string {
	if msg == nil {
		return ""
	}

	switch c := msg["content"].(type) {
	case string:
		return c
	case []any:
		var b strings.Builder

		for _, item := range c {
			block, _ := item.(map[string]any)
			if block == nil {
				continue
			}

			if t, _ := block["type"].(string); t == "text" || t == "" {
				if text, _ := block["text"].(string); text != "" {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}

					b.WriteString(text)
				}
			}
		}

		return b.String()
	}

	return ""
}

func collectSystemText(req map[string]any, msgs []any) string {
	var parts []string

	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		if msg == nil {
			continue
		}

		if role, _ := msg["role"].(string); role == "system" {
			parts = append(parts, messageText(msg))
		}
	}

	switch s := req["system"].(type) {
	case string:
		parts = append(parts, s)
	case []any:
		for _, item := range s {
			block, _ := item.(map[string]any)
			if block == nil {
				continue
			}

			if t, _ := block["type"].(string); t == "text" || t == "" {
				if text, _ := block["text"].(string); text != "" {
					parts = append(parts, text)
				}
			}
		}
	}

	return strings.Join(parts, " ")
}
