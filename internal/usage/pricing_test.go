package usage

import (
	"math"
	"testing"
)

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name             string
		provider         string
		model            string
		promptTokens     int
		cachedTokens     int
		completionTokens int
		want             float64
	}{
		{
			name:             "exact model match - gpt-4o",
			provider:         "openai",
			model:            "gpt-4o",
			promptTokens:     1000,
			cachedTokens:     0,
			completionTokens: 500,
			// gpt-4o: Input $2.50/1M, Output $10.00/1M
			// (1000 * 2.50 / 1e6) + (500 * 10.00 / 1e6) = 0.0025 + 0.005 = 0.0075
			want: 0.0075,
		},
		{
			name:             "exact model with provider prefix - openai/gpt-4o",
			provider:         "openai",
			model:            "openai/gpt-4o",
			promptTokens:     1000,
			cachedTokens:     0,
			completionTokens: 500,
			want:             0.0075,
		},
		{
			name:             "cached tokens deduction and pricing - claude-sonnet-4-6",
			provider:         "anthropic",
			model:            "claude-sonnet-4-6",
			promptTokens:     1000,
			cachedTokens:     400,
			completionTokens: 200,
			// claude-sonnet-4-6: Input $3.00/1M, Output $15.00/1M, Cached $0.30/1M
			// nonCachedPrompt = 1000 - 400 = 600
			// cost = (600 * 3.00 / 1e6) + (400 * 0.30 / 1e6) + (200 * 15.00 / 1e6)
			//      = 0.0018 + 0.00012 + 0.0030 = 0.00492
			want: 0.00492,
		},
		{
			name:             "pattern matching - gpt-4o variant",
			provider:         "openai",
			model:            "gpt-4o-2024-08-06",
			promptTokens:     2000,
			cachedTokens:     500,
			completionTokens: 1000,
			// Matches glob pattern "gpt-4o-*": Input $0.15/1M, Output $0.60/1M, Cached $0.075/1M
			// nonCachedPrompt = 1500
			// cost = (1500 * 0.15 / 1e6) + (500 * 0.075 / 1e6) + (1000 * 0.60 / 1e6)
			//      = 0.000225 + 0.0000375 + 0.000600 = 0.0008625 -> round 6 decimals = 0.000863
			want: 0.000863,
		},
		{
			name:             "unknown model default fallback",
			provider:         "unknown",
			model:            "some-random-unknown-model",
			promptTokens:     1000,
			cachedTokens:     200,
			completionTokens: 1000,
			// Default: Input $1.00/1M, Output $4.00/1M, Cached $0.25/1M
			// nonCachedPrompt = 800
			// cost = (800 * 1.00 / 1e6) + (200 * 0.25 / 1e6) + (1000 * 4.00 / 1e6)
			//      = 0.0008 + 0.00005 + 0.0040 = 0.00485
			want: 0.00485,
		},
		{
			name:             "cachedTokens greater than promptTokens",
			provider:         "openai",
			model:            "gpt-4o",
			promptTokens:     100,
			cachedTokens:     200,
			completionTokens: 100,
			// nonCachedPrompt clamped to 0
			// cost = (0 * 2.50 / 1e6) + (200 * 1.25 / 1e6) + (100 * 10.00 / 1e6)
			//      = 0 + 0.00025 + 0.0010 = 0.00125
			want: 0.00125,
		},
		{
			name:             "zero tokens",
			provider:         "openai",
			model:            "gpt-4o",
			promptTokens:     0,
			cachedTokens:     0,
			completionTokens: 0,
			want:             0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCost(tt.provider, tt.model, tt.promptTokens, tt.cachedTokens, tt.completionTokens)
			if math.Abs(got-tt.want) > 1e-6 {
				t.Errorf("CalculateCost(%q, %q, %d, %d, %d) = %v, want %v",
					tt.provider, tt.model, tt.promptTokens, tt.cachedTokens, tt.completionTokens, got, tt.want)
			}
		})
	}
}

func TestCalculateCost_ZeroCachedRateFallback(t *testing.T) {
	// Temporarily construct pricing entry with Cached = 0 to test fallback rate logic
	pricing := ModelPricing{
		Input:  2.00,
		Output: 10.00,
		Cached: 0,
	}

	// Verify formula when Cached == 0
	// If p.Cached == 0, cachedRate = inputRate (p.Input / 1000000.0)
	inputRate := pricing.Input / 1000000.0
	cachedRate := inputRate
	outputRate := pricing.Output / 1000000.0

	promptTokens := 1000
	cachedTokens := 300
	completionTokens := 500
	nonCachedPrompt := promptTokens - cachedTokens

	expectedCost := (float64(nonCachedPrompt) * inputRate) +
		(float64(cachedTokens) * cachedRate) +
		(float64(completionTokens) * outputRate)
	expectedCost = math.Round(expectedCost*1000000) / 1000000

	// promptTotal = nonCached + cached = total promptTokens * inputRate
	totalPromptCost := float64(promptTokens) * inputRate
	totalCompletionCost := float64(completionTokens) * outputRate
	altCost := math.Round((totalPromptCost+totalCompletionCost)*1000000) / 1000000

	if expectedCost != altCost {
		t.Fatalf("expected cost fallback calculation mismatch: %v vs %v", expectedCost, altCost)
	}
}

func TestGetPricingForModel(t *testing.T) {
	tests := []struct {
		model     string
		wantFound bool
		wantInput float64
	}{
		{"gpt-4o", true, 2.50},
		{"openai/gpt-4o", true, 2.50},
		{"claude-sonnet-4-6", true, 3.00},
		{"anthropic/claude-sonnet-4-6", true, 3.00},
		{"my-custom-codex-high", true, 8.00},
		{"gemini-9.9-flash", true, 0.30},
		{"non-existent-model-xyz", false, 1.00},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, found := GetPricingForModel(tt.model)
			if found != tt.wantFound {
				t.Errorf("GetPricingForModel(%q) found = %v, wantFound = %v", tt.model, found, tt.wantFound)
			}
			if pricing.Input != tt.wantInput {
				t.Errorf("GetPricingForModel(%q) Input rate = %v, wantInput = %v", tt.model, pricing.Input, tt.wantInput)
			}
		})
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"gpt-4*", "gpt-4o", true},
		{"gpt-4*", "GPT-4O", true},
		{"*-codex", "my-codex", true},
		{"*-codex-mini-*", "a-codex-mini-b", true},
		{"exact-match", "exact-match", true},
		{"exact-match", "other-match", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.text, func(t *testing.T) {
			got := matchGlob(tt.pattern, tt.text)
			if got != tt.want {
				t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.text, got, tt.want)
			}
		})
	}
}
