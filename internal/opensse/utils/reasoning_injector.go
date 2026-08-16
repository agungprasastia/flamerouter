package utils

import "encoding/json"

// InjectReasoning adds reasoning/thinking content into an SSE chunk (OpenAI shape).
func InjectReasoning(chunk []byte, reasoning string) []byte {
	if reasoning == "" || len(chunk) == 0 {
		return chunk
	}

	var c map[string]any
	if err := json.Unmarshal(chunk, &c); err != nil {
		return chunk
	}

	choices, _ := c["choices"].([]any)
	if len(choices) == 0 {
		return chunk
	}

	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return chunk
	}

	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		delta = map[string]any{}
		choice["delta"] = delta
	}

	delta["reasoning_content"] = reasoning
	choices[0] = choice
	c["choices"] = choices

	out, err := json.Marshal(c)
	if err != nil {
		return chunk
	}

	return out
}
