package utils

import "encoding/json"

func injectReasoningDelta(choices []any, reasoning string) bool {
	if len(choices) == 0 {
		return false
	}

	choice, ok := choices[0].(map[string]any)
	if !ok || choice == nil {
		return false
	}

	delta, ok := choice["delta"].(map[string]any)
	if !ok || delta == nil {
		delta = map[string]any{}
		choice["delta"] = delta
	}

	delta["reasoning_content"] = reasoning
	choices[0] = choice

	return true
}

// InjectReasoning adds reasoning/thinking content into an SSE chunk (OpenAI shape).
func InjectReasoning(chunk []byte, reasoning string) []byte {
	if reasoning == "" || len(chunk) == 0 {
		return chunk
	}

	var c map[string]any
	if err := json.Unmarshal(chunk, &c); err != nil {
		return chunk
	}

	choices, ok := c["choices"].([]any)
	if !ok || !injectReasoningDelta(choices, reasoning) {
		return chunk
	}

	c["choices"] = choices

	out, err := json.Marshal(c)
	if err != nil {
		return chunk
	}

	return out
}
