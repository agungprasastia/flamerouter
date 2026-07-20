package handlers

import "testing"

func TestEstimateAnthropicInputTokens(t *testing.T) {
	n := estimateAnthropicInputTokens(map[string]any{
		"system": "abcd", // 4 chars → 1 token
		"messages": []any{
			map[string]any{"role": "user", "content": "hello world!!"}, // 13 chars
		},
	})
	// (4+13)/4 = 4.25 → 5
	if n != 5 {
		t.Fatalf("got %d want 5", n)
	}
}
