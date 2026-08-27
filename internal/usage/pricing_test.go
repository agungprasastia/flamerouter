package usage

import (
	"math"
	"testing"
)

type modelPricingTestCase struct {
	name          string
	model         string
	expectedInput float64
	expectedOut   float64
	expectedOK    bool
}

func runPricingTestCases(t *testing.T, tests []modelPricingTestCase) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pricing, ok := GetPricingForModel(tt.model)

			if ok != tt.expectedOK {
				t.Errorf("GetPricingForModel(%q) ok = %v, expectedOK = %v", tt.model, ok, tt.expectedOK)
			}

			if pricing.Input != tt.expectedInput {
				t.Errorf("GetPricingForModel(%q) Input = %v, expected %v", tt.model, pricing.Input, tt.expectedInput)
			}

			if pricing.Output != tt.expectedOut {
				t.Errorf("GetPricingForModel(%q) Output = %v, expected %v", tt.model, pricing.Output, tt.expectedOut)
			}
		})
	}
}

func TestGetPricingForModel_ExactAndPrefix(t *testing.T) {
	tests := []modelPricingTestCase{
		{
			name:          "exact match - gpt-4o",
			model:         "gpt-4o",
			expectedInput: 2.50,
			expectedOut:   10.00,
			expectedOK:    true,
		},
		{
			name:          "exact match - claude",
			model:         "claude-3-5-sonnet-20241022",
			expectedInput: 3.00,
			expectedOut:   15.00,
			expectedOK:    true,
		},
		{
			name:          "exact match - deepseek",
			model:         "deepseek-chat",
			expectedInput: 0.14,
			expectedOut:   0.28,
			expectedOK:    true,
		},
		{
			name:          "provider prefix - openai/gpt-4o",
			model:         "openai/gpt-4o",
			expectedInput: 2.50,
			expectedOut:   10.00,
			expectedOK:    true,
		},
		{
			name:          "provider prefix - anthropic/claude-3-5-sonnet-20241022",
			model:         "anthropic/claude-3-5-sonnet-20241022",
			expectedInput: 3.00,
			expectedOut:   15.00,
			expectedOK:    true,
		},
		{
			name:          "multiple slashes - org/provider/gpt-4o",
			model:         "org/provider/gpt-4o",
			expectedInput: 2.50,
			expectedOut:   10.00,
			expectedOK:    true,
		},
	}

	runPricingTestCases(t, tests)
}

func TestGetPricingForModel_PatternsAndFallback(t *testing.T) {
	tests := []modelPricingTestCase{
		{
			name:          "pattern match - claude-opus-*",
			model:         "claude-opus-4.7",
			expectedInput: 5.00,
			expectedOut:   25.00,
			expectedOK:    true,
		},
		{
			name:          "pattern match - gpt-5.6-*",
			model:         "gpt-5.6-custom-variant",
			expectedInput: 2.50,
			expectedOut:   15.00,
			expectedOK:    true,
		},
		{
			name:          "pattern match - deepseek-*",
			model:         "deepseek-v7",
			expectedInput: 0.14,
			expectedOut:   0.28,
			expectedOK:    true,
		},
		{
			name:          "pattern match with prefix - provider/deepseek-v7",
			model:         "provider/deepseek-v7",
			expectedInput: 0.14,
			expectedOut:   0.28,
			expectedOK:    true,
		},
		{
			name:          "unknown model - fallback",
			model:         "unknown-model-xyz",
			expectedInput: 1.00,
			expectedOut:   4.00,
			expectedOK:    false,
		},
		{
			name:          "unknown model with prefix - fallback",
			model:         "some-provider/unknown-model-xyz",
			expectedInput: 1.00,
			expectedOut:   4.00,
			expectedOK:    false,
		},
		{
			name:          "empty model string",
			model:         "",
			expectedInput: 1.00,
			expectedOut:   4.00,
			expectedOK:    false,
		},
		{
			name:          "trailing slash model string",
			model:         "openai/",
			expectedInput: 1.00,
			expectedOut:   4.00,
			expectedOK:    false,
		},
	}

	runPricingTestCases(t, tests)
}

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name             string
		provider         string
		model            string
		promptTokens     int
		cachedTokens     int
		completionTokens int
		expectedCost     float64
	}{
		{
			name:             "known model no cached tokens",
			provider:         "openai",
			model:            "gpt-4o",
			promptTokens:     1000000,
			cachedTokens:     0,
			completionTokens: 1000000,
			expectedCost:     12.50,
		},
		{
			name:             "known model with cached tokens",
			provider:         "openai",
			model:            "gpt-4o",
			promptTokens:     1000000,
			cachedTokens:     400000,
			completionTokens: 1000000,
			expectedCost:     12.00,
		},
		{
			name:             "cached tokens exceed prompt tokens",
			provider:         "openai",
			model:            "gpt-4o",
			promptTokens:     500,
			cachedTokens:     600,
			completionTokens: 1000,
			expectedCost:     0.01075,
		},
		{
			name:             "unknown model fallback pricing",
			provider:         "custom",
			model:            "unknown-model",
			promptTokens:     1000000,
			cachedTokens:     0,
			completionTokens: 1000000,
			expectedCost:     5.00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := CalculateCost(tt.provider, tt.model, tt.promptTokens, tt.cachedTokens, tt.completionTokens)

			if math.Abs(cost-tt.expectedCost) > 1e-6 {
				t.Errorf("CalculateCost(%q, %q, %d, %d, %d) = %v, expected %v",
					tt.provider, tt.model, tt.promptTokens, tt.cachedTokens, tt.completionTokens, cost, tt.expectedCost)
			}
		})
	}
}
