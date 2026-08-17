package concerns

import (
	"encoding/json"
	"testing"
)

func TestApplyThinkingLevel_ThinkingSuffix(t *testing.T) {
	body := []byte(`{"model":"claude-3-opus:thinking","messages":[]}`)

	result, model := ApplyThinkingLevel(body, "claude-3-opus:thinking", "claude", "claude", nil)
	if model != "claude-3-opus" {
		t.Fatalf("expected cleaned model, got %s", model)
	}

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	if m["thinking"] == nil {
		t.Fatal("expected thinking block added")
	}

	thinking, ok := m["thinking"].(map[string]any)
	if !ok {
		t.Fatal("expected thinking map")
	}

	if thinking["type"] != "enabled" {
		t.Fatalf("expected type enabled, got %v", thinking["type"])
	}

	if m["model"] != "claude-3-opus" {
		t.Fatalf("expected body model cleaned, got %v", m["model"])
	}
}

func TestApplyThinkingLevel_OpenAI(t *testing.T) {
	body := []byte(`{"model":"gpt-5:thinking","messages":[]}`)

	result, model := ApplyThinkingLevel(body, "gpt-5:thinking", "openai", "openai", nil)
	if model != "gpt-5" {
		t.Fatalf("expected gpt-5, got %s", model)
	}

	var m map[string]any

	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	if m["reasoning_effort"] == nil {
		t.Fatal("expected reasoning_effort")
	}
}

func TestApplyThinkingLevel_Gemini(t *testing.T) {
	body := []byte(`{"model":"gemini-2.5-pro:thinking","contents":[]}`)

	result, model := ApplyThinkingLevel(body, "gemini-2.5-pro:thinking", "gemini", "gemini", nil)
	if model != "gemini-2.5-pro" {
		t.Fatalf("expected cleaned model, got %s", model)
	}

	var m map[string]any

	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	gc, ok := m["generationConfig"].(map[string]any)
	if !ok || gc["thinkingConfig"] == nil {
		t.Fatal("expected generationConfig.thinkingConfig")
	}
}

func TestApplyThinkingLevel_ProviderThinkingOn(t *testing.T) {
	body := []byte(`{"model":"claude-3-opus","messages":[]}`)
	pt := map[string]any{"mode": "on"}

	result, model := ApplyThinkingLevel(body, "claude-3-opus", "claude", "claude", pt)
	if model != "claude-3-opus" {
		t.Fatalf("model unchanged expected, got %s", model)
	}

	var m map[string]any

	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	thinking, ok := m["thinking"].(map[string]any)
	if !ok || thinking == nil {
		t.Fatal("expected thinking from providerThinking")
	}

	if thinking["type"] != "enabled" {
		t.Fatalf("expected enabled, got %v", thinking["type"])
	}

	if thinking["budget_tokens"] != float64(10000) && thinking["budget_tokens"] != 10000 {
		t.Fatalf("expected budget_tokens 10000, got %v", thinking["budget_tokens"])
	}
}

func TestApplyThinkingLevel_ProviderThinkingOff(t *testing.T) {
	body := []byte(`{"model":"claude-3-opus","messages":[]}`)
	pt := map[string]any{"mode": "off"}
	result, _ := ApplyThinkingLevel(body, "claude-3-opus", "claude", "claude", pt)

	var m map[string]any

	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	thinking, ok := m["thinking"].(map[string]any)
	if !ok || thinking == nil {
		t.Fatal("expected thinking disabled")
	}

	if thinking["type"] != "disabled" {
		t.Fatalf("expected type disabled, got %v", thinking["type"])
	}

	if m["reasoning_effort"] != nil {
		t.Fatalf("off must not set reasoning_effort, got %v", m["reasoning_effort"])
	}
}

func TestApplyThinkingLevel_ProviderThinkingNone(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[]}`)
	pt := map[string]any{"mode": "none"}
	result, _ := ApplyThinkingLevel(body, "gpt-5", "openai", "openai", pt)

	var m map[string]any

	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	if m["reasoning_effort"] != "none" {
		t.Fatalf("expected reasoning_effort none, got %v", m["reasoning_effort"])
	}

	if m["thinking"] != nil {
		t.Fatalf("none must not set thinking disabled, got %v", m["thinking"])
	}
}

func TestApplyThinkingLevel_ClientThinkingPlusProviderEffort(t *testing.T) {
	// Client already set thinking; provider mode high still injects reasoning_effort.
	body := []byte(`{"model":"gpt-5","messages":[],"thinking":{"type":"enabled","budget_tokens":5000}}`)
	pt := map[string]any{"mode": "high"}
	result, _ := ApplyThinkingLevel(body, "gpt-5", "openai", "openai", pt)

	var m map[string]any

	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	thinking, ok := m["thinking"].(map[string]any)
	if !ok || thinking == nil || thinking["type"] != "enabled" {
		t.Fatalf("client thinking must remain, got %v", m["thinking"])
	}

	if m["reasoning_effort"] != "high" {
		t.Fatalf("expected reasoning_effort high despite client thinking, got %v", m["reasoning_effort"])
	}
}

func TestApplyThinkingLevel_ProviderThinkingEffort(t *testing.T) {
	body := []byte(`{"model":"gpt-5","messages":[]}`)
	pt := map[string]any{"mode": "high"}
	result, _ := ApplyThinkingLevel(body, "gpt-5", "openai", "openai", pt)

	var m map[string]any

	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	if m["reasoning_effort"] != "high" {
		t.Fatalf("expected reasoning_effort high, got %v", m["reasoning_effort"])
	}
}

func TestApplyThinkingLevel_NoOp(t *testing.T) {
	body := []byte(`{"model":"claude-3-opus","messages":[]}`)

	result, model := ApplyThinkingLevel(body, "claude-3-opus", "claude", "claude", nil)
	if model != "claude-3-opus" {
		t.Fatalf("got %s", model)
	}

	var m map[string]any

	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}

	if m["thinking"] != nil {
		t.Fatal("expected no thinking without suffix or providerThinking")
	}
}
