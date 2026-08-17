package handlers

import (
	"encoding/json"
	"math"
	"net/http"
)

// CountTokens handles POST /v1/messages/count_tokens.
// Parity with 9router: local char/4 estimator (no upstream).
func CountTokens(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck // best effort encode
		"input_tokens": estimateAnthropicInputTokens(body),
	})
}

func estimateAnthropicInputTokens(body map[string]any) int {
	total := countValueChars(body["system"]) + countValueChars(body["tools"])

	if msgs, ok := body["messages"].([]any); ok {
		for _, m := range msgs {
			total += countMessageChars(m)
		}
	}

	return int(math.Ceil(float64(total) / 4))
}

func countMessageChars(message any) int {
	msg, ok := message.(map[string]any)
	if !ok {
		return 0
	}

	content := msg["content"]
	switch c := content.(type) {
	case string:
		return len(c)
	case []any:
		n := 0
		for _, b := range c {
			n += countContentBlockChars(b)
		}

		return n
	default:
		return countValueChars(content)
	}
}

func countContentBlockChars(block any) int {
	if block == nil {
		return 0
	}

	if s, ok := block.(string); ok {
		return len(s)
	}

	m, ok := block.(map[string]any)
	if !ok {
		return countValueChars(block)
	}

	switch m["type"] {
	case "text":
		return countValueChars(m["text"])
	case "tool_use":
		return countValueChars(m["name"]) + countValueChars(m["input"])
	case "tool_result":
		return countValueChars(m["content"])
	case "thinking":
		return countValueChars(m["thinking"])
	default:
		return countValueChars(m)
	}
}

func countValueChars(value any) int {
	if value == nil {
		return 0
	}

	switch v := value.(type) {
	case string:
		return len(v)
	case []any:
		return countSliceChars(v)
	case map[string]any:
		return countMapChars(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return 0
		}

		return len(b)
	}
}

func countSliceChars(v []any) int {
	n := 0
	for _, item := range v {
		n += countValueChars(item)
	}

	return n
}

func countMapChars(v map[string]any) int {
	n := 0
	for k, item := range v {
		n += len(k) + countValueChars(item)
	}

	return n
}
