package concerns

// DefaultMaxTokens and DefaultMinTokens define fallback limits for token generation.
const (
	DefaultMaxTokens = 64000
	DefaultMinTokens = 32000
)

func parseMaxTokensVal(body map[string]any) int {
	if body == nil {
		return DefaultMaxTokens
	}

	switch v := body["max_tokens"].(type) {
	case float64:
		if int(v) > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	}

	return DefaultMaxTokens
}

func adjustTokensForToolsAndThinking(body map[string]any, current int) int {
	if body == nil {
		return current
	}

	maxTokens := current
	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		if maxTokens < DefaultMinTokens {
			maxTokens = DefaultMinTokens
		}
	}

	if thinking, ok := body["thinking"].(map[string]any); ok {
		budget := extractBudget(thinking)
		if budget > 0 && maxTokens <= budget {
			maxTokens = budget + 1024
		}
	}

	return maxTokens
}

func extractBudget(thinking map[string]any) int {
	switch b := thinking["budget_tokens"].(type) {
	case float64:
		return int(b)
	case int:
		return b
	default:
		return 0
	}
}

// AdjustMaxTokens matches 9router formats/maxTokens.js.
func AdjustMaxTokens(body map[string]any, ceiling int) int {
	if ceiling <= 0 {
		ceiling = DefaultMaxTokens
	}

	maxTokens := parseMaxTokensVal(body)
	maxTokens = adjustTokensForToolsAndThinking(body, maxTokens)

	if maxTokens > ceiling {
		maxTokens = ceiling
	}

	return maxTokens
}
