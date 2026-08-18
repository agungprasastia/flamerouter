package executor

import (
	"testing"
)

func TestGrokCliCleanModelName(t *testing.T) {
	cases := []struct {
		input          string
		expectedModel  string
		expectedEffort string
	}{
		{
			input:          "gcli/grok-4.5",
			expectedModel:  "grok-4.5",
			expectedEffort: "",
		},
		{
			input:          "grok-cli/grok-4.5",
			expectedModel:  "grok-4.5",
			expectedEffort: "",
		},
		{
			input:          "gcli/grok-4.5-high",
			expectedModel:  "grok-4.5",
			expectedEffort: "high",
		},
		{
			input:          "grok-4.5-low",
			expectedModel:  "grok-4.5",
			expectedEffort: "low",
		},
		{
			input:          "grok-4.5",
			expectedModel:  "grok-4.5",
			expectedEffort: "",
		},
		{
			input:          "gcli/grok-build",
			expectedModel:  "grok-build",
			expectedEffort: "",
		},
	}

	for _, tc := range cases {
		m, eff := resolveGrokModelEffort(tc.input, map[string]any{"model": tc.input})
		if m != tc.expectedModel {
			t.Errorf("input %q: expected model %q, got %q", tc.input, tc.expectedModel, m)
		}

		if eff != tc.expectedEffort {
			t.Errorf("input %q: expected effort %q, got %q", tc.input, tc.expectedEffort, eff)
		}
	}
}

func TestGrokCliTransform(t *testing.T) {
	exec := &GrokCliExecutor{
		Base: Base{
			Provider: "grok-cli",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "",
			BaseURLs: nil,
		},
	}
	body := map[string]any{
		"model": "gcli/grok-4.5",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}

	transformed := exec.transform("gcli/grok-4.5", body)

	if transformed["model"] != "grok-4.5" {
		t.Fatalf("expected model grok-4.5, got %v", transformed["model"])
	}
}

func TestGrokCliTransformReasoning(t *testing.T) {
	exec := &GrokCliExecutor{
		Base: Base{
			Provider: "grok-cli",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "",
			BaseURLs: nil,
		},
	}
	body := map[string]any{
		"model": "gcli/grok-4.5-high",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
		},
	}

	transformed := exec.transform("gcli/grok-4.5-high", body)

	reasoning, ok := transformed["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("expected reasoning map, got %v", transformed["reasoning"])
	}

	if reasoning["effort"] != "high" {
		t.Fatalf("expected reasoning.effort high, got %v", reasoning["effort"])
	}
}
