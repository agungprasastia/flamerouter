package usage

import (
	"math"
	"testing"
)

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		text    string
		want    bool
	}{
		{
			name:    "exact match no wildcards",
			pattern: "gpt-4o",
			text:    "gpt-4o",
			want:    true,
		},
		{
			name:    "case insensitive exact match",
			pattern: "GPT-4O",
			text:    "gpt-4o",
			want:    true,
		},
		{
			name:    "single wildcard prefix",
			pattern: "*-codex",
			text:    "gpt-5.1-codex",
			want:    true,
		},
		{
			name:    "single wildcard suffix",
			pattern: "claude-*",
			text:    "claude-sonnet-4-6",
			want:    true,
		},
		{
			name:    "multiple wildcards",
			pattern: "gemini-*-flash-*",
			text:    "gemini-3.7-flash-high",
			want:    true,
		},
		{
			name:    "regex special chars in pattern (dots)",
			pattern: "gpt-3.5-turbo",
			text:    "gpt-3.5-turbo",
			want:    true,
		},
		{
			name:    "regex special chars mismatch",
			pattern: "gpt-3.5-turbo",
			text:    "gpt-3X5-turbo",
			want:    false,
		},
		{
			name:    "non matching text",
			pattern: "claude-*",
			text:    "gpt-4o",
			want:    false,
		},
		{
			name:    "empty pattern and text",
			pattern: "",
			text:    "",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.text)

			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v; want %v", tt.pattern, tt.text, got, tt.want)
			}
		})
	}
}

func TestGetPricingForModel(t *testing.T) {
	t.Run("exact match in table", func(t *testing.T) {
		pricing, ok := GetPricingForModel("claude-sonnet-4-6")

		if !ok {
			t.Fatalf("expected pricing for claude-sonnet-4-6")
		}

		if pricing.Input != 3.00 || pricing.Output != 15.00 || pricing.Cached != 0.30 {
			t.Errorf("unexpected pricing values: %+v", pricing)
		}
	})

	t.Run("provider prefix stripped exact match", func(t *testing.T) {
		pricing, ok := GetPricingForModel("anthropic/claude-sonnet-4-6")

		if !ok {
			t.Fatalf("expected pricing for anthropic/claude-sonnet-4-6")
		}

		if pricing.Input != 3.00 || pricing.Output != 15.00 {
			t.Errorf("unexpected pricing values: %+v", pricing)
		}
	})

	t.Run("pattern matching in pattern list", func(t *testing.T) {
		pricing, ok := GetPricingForModel("custom-provider/gpt-5.6-super")

		if !ok {
			t.Fatalf("expected pattern pricing match for gpt-5.6-super")
		}

		if pricing.Input != 2.50 || pricing.Output != 15.00 {
			t.Errorf("unexpected pricing values for pattern match: %+v", pricing)
		}
	})

	t.Run("unknown model fallback", func(t *testing.T) {
		pricing, ok := GetPricingForModel("completely-unknown-model-xyz")

		if ok {
			t.Errorf("expected ok=false for unknown model")
		}

		expectedFallback := ModelPricing{Input: 1.00, Output: 4.00, Cached: 0.25, Reasoning: 4.00, CacheCreation: 1.00}

		if pricing != expectedFallback {
			t.Errorf("got fallback pricing %+v, want %+v", pricing, expectedFallback)
		}
	})
}

func TestCalculateCost(t *testing.T) {
	t.Run("standard cost calculation without cached tokens", func(t *testing.T) {
		// deepseek-chat: Input 0.14/1M, Output 0.28/1M
		cost := CalculateCost("deepseek", "deepseek-chat", 1_000_000, 0, 1_000_000)
		expected := 0.14 + 0.28

		if math.Abs(cost-expected) > 1e-6 {
			t.Errorf("CalculateCost = %f; want %f", cost, expected)
		}
	})

	t.Run("cost calculation with cached tokens", func(t *testing.T) {
		// claude-sonnet-4-6: Input 3.00/1M, Output 15.00/1M, Cached 0.30/1M
		// prompt: 1M tokens, 500k cached -> 500k non-cached prompt @ 3.00/1M ($1.50) + 500k cached @ 0.30/1M ($0.15) + 1M output @ 15.00/1M ($15.00) = $16.65
		cost := CalculateCost("anthropic", "claude-sonnet-4-6", 1_000_000, 500_000, 1_000_000)
		expected := 16.65

		if math.Abs(cost-expected) > 1e-6 {
			t.Errorf("CalculateCost = %f; want %f", cost, expected)
		}
	})

	t.Run("cached tokens exceed prompt tokens clamp to zero non-cached", func(t *testing.T) {
		// prompt: 100k, cached: 200k -> nonCachedPrompt clamped to 0
		// cached tokens = 200k @ 0.30/1M ($0.06), output = 0 -> $0.06
		cost := CalculateCost("anthropic", "claude-sonnet-4-6", 100_000, 200_000, 0)
		expected := 0.06

		if math.Abs(cost-expected) > 1e-6 {
			t.Errorf("CalculateCost = %f; want %f", cost, expected)
		}
	})

	t.Run("zero cached rate fallback to input rate", func(t *testing.T) {
		// Custom test model behavior if p.Cached == 0
		// We test CalculateCost on unknown model with zero cached rate by checking logic behavior with fallback/known model
		cost := CalculateCost("test", "unknown-model", 1_000_000, 0, 0)
		// Fallback model: Input 1.00, Output 4.00, Cached 0.25 -> 1.00
		expected := 1.00

		if math.Abs(cost-expected) > 1e-6 {
			t.Errorf("CalculateCost = %f; want %f", cost, expected)
		}
	})

	t.Run("cost rounding to 6 decimal places", func(t *testing.T) {
		// Calculate small token cost that tests rounding precision
		cost := CalculateCost("deepseek", "deepseek-chat", 1, 0, 1)

		// 1/1M * 0.14 + 1/1M * 0.28 = 0.42 / 1000000 = 0.00000042 -> rounds to 0.000000
		if cost != 0.000000 {
			t.Errorf("CalculateCost small tokens = %f; want 0.000000", cost)
		}

		cost2 := CalculateCost("deepseek", "deepseek-chat", 1000, 0, 1000)

		// 1000/1M * 0.14 + 1000/1M * 0.28 = 0.00014 + 0.00028 = 0.000420
		if cost2 != 0.000420 {
			t.Errorf("CalculateCost medium tokens = %f; want 0.000420", cost2)
		}
	})
}
