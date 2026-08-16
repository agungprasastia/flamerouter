package concerns

const (
	DefaultMaxTokens = 64000
	DefaultMinTokens = 32000
)

// AdjustMaxTokens matches 9router formats/maxTokens.js.
func AdjustMaxTokens(body map[string]any, ceiling int) int {
	if ceiling <= 0 {
		ceiling = DefaultMaxTokens
	}

	maxTokens := DefaultMaxTokens

	if body != nil {
		switch v := body["max_tokens"].(type) {
		case float64:
			if int(v) > 0 {
				maxTokens = int(v)
			}
		case int:
			if v > 0 {
				maxTokens = v
			}
		}
	}

	if body != nil {
		if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
			if maxTokens < DefaultMinTokens {
				maxTokens = DefaultMinTokens
			}
		}

		if thinking, ok := body["thinking"].(map[string]any); ok {
			var budget int
			switch b := thinking["budget_tokens"].(type) {
			case float64:
				budget = int(b)
			case int:
				budget = b
			}

			if budget > 0 && maxTokens <= budget {
				maxTokens = budget + 1024
			}
		}
	}

	if maxTokens > ceiling {
		maxTokens = ceiling
	}

	return maxTokens
}
