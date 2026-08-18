package usage

import (
	"testing"
)

func TestExtractUsageFromChunk(t *testing.T) {
	// OpenAI chunk
	openAIChunk := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":     float64(45),
			"completion_tokens": float64(10),
		},
	}
	u, ok := ExtractUsageFromChunk(openAIChunk)

	if !ok || u.PromptTokens != 45 || u.CompletionTokens != 10 {
		t.Fatalf("expected 45/10, got %+v", u)
	}

	// Claude chunk
	claudeChunk := map[string]any{
		"message": map[string]any{
			"usage": map[string]any{
				"input_tokens":  float64(30),
				"output_tokens": float64(15),
			},
		},
	}
	u, ok = ExtractUsageFromChunk(claudeChunk)

	if !ok || u.PromptTokens != 30 || u.CompletionTokens != 15 {
		t.Fatalf("expected 30/15, got %+v", u)
	}

	// Gemini chunk
	geminiChunk := map[string]any{
		"usageMetadata": map[string]any{
			"promptTokenCount":     float64(50),
			"candidatesTokenCount": float64(20),
		},
	}
	u, ok = ExtractUsageFromChunk(geminiChunk)

	if !ok || u.PromptTokens != 50 || u.CompletionTokens != 20 {
		t.Fatalf("expected 50/20, got %+v", u)
	}

	// DeepSeek-style chunk (prompt_cache_hit_tokens)
	deepseekChunk := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":            float64(100),
			"completion_tokens":        float64(8),
			"prompt_cache_hit_tokens":  float64(80),
			"prompt_cache_miss_tokens": float64(20),
		},
	}
	u, ok = ExtractUsageFromChunk(deepseekChunk)

	if !ok || u.PromptTokens != 100 || u.CompletionTokens != 8 || u.CachedTokens != 80 {
		t.Fatalf("expected 100/8 cached=80, got %+v", u)
	}

	// Kiro-style chunk (cache_read_input_tokens inside usage)
	kiroChunk := map[string]any{
		"usage": map[string]any{
			"prompt_tokens":               float64(90),
			"completion_tokens":           float64(5),
			"cache_read_input_tokens":     float64(60),
			"cache_creation_input_tokens": float64(10),
		},
	}
	u, ok = ExtractUsageFromChunk(kiroChunk)

	if !ok || u.PromptTokens != 90 || u.CompletionTokens != 5 || u.CachedTokens != 60 {
		t.Fatalf("expected 90/5 cached=60, got %+v", u)
	}
}

func TestEstimateTokens(t *testing.T) {
	body := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hello world"}]}`)
	prompt := EstimateInputTokens(body)

	if prompt <= 0 {
		t.Fatalf("expected positive prompt tokens, got %d", prompt)
	}

	completion := EstimateOutputTokens(100)
	if completion != 25 {
		t.Fatalf("expected 25 completion tokens, got %d", completion)
	}
}
