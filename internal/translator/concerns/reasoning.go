package concerns

// ReasoningDelta builds a streaming reasoning delta map.
func ReasoningDelta(text string, withRole bool) map[string]any {
	delta := map[string]any{
		"type":              "reasoning_content",
		"reasoning_content": text,
	}
	if withRole {
		delta["role"] = "assistant"
	}

	return delta
}

func extractDetailsItemText(item any) string {
	switch v := item.(type) {
	case string:
		return v
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			return t
		}

		if c, ok := v["content"].(string); ok {
			return c
		}
	}

	return ""
}

// ExtractReasoningText extracts reasoning content from a delta payload.
func ExtractReasoningText(delta map[string]any) string {
	if delta == nil {
		return ""
	}

	if rc, ok := delta["reasoning_content"].(string); ok && rc != "" {
		return rc
	}

	if r, ok := delta["reasoning"].(string); ok && r != "" {
		return r
	}

	if rd, ok := delta["reasoning_details"].([]any); ok {
		var text string
		for _, item := range rd {
			text += extractDetailsItemText(item)
		}

		return text
	}

	return ""
}

// HasReasoningIntent checks if reasoning is requested in the request body.
func HasReasoningIntent(body map[string]any) bool {
	if body == nil {
		return false
	}

	if thinking, ok := body["thinking"]; ok && thinking != nil {
		return true
	}

	if think, ok := body["think"]; ok && think != nil {
		return true
	}

	return false
}

// ExtractThinkingConfig extracts thinking map configuration from request.
func ExtractThinkingConfig(body map[string]any) map[string]any {
	if body == nil {
		return nil
	}

	if thinking, ok := body["thinking"].(map[string]any); ok {
		return thinking
	}

	if think, ok := body["think"].(map[string]any); ok {
		return think
	}

	return nil
}
