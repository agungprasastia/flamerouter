package provider_test

import (
	"testing"

	"flamerouter/internal/provider"
)

func TestProviderRegistryModelsParity(t *testing.T) {
	// 1. Check top providers existence
	expectedProviders := []string{
		"openai", "claude", "codex", "cursor", "kiro", "antigravity",
		"github", "gemini", "deepseek", "groq", "openrouter", "xai",
		"mistral", "alicode", "cohere", "fireworks", "together", "ollama",
		"trae", "windsurf", "devin-cli",
	}
	for _, pid := range expectedProviders {
		p := provider.GetProvider(pid)
		if p == nil {
			t.Fatalf("provider %q not found in registry", pid)
		}
		if len(p.Models) == 0 {
			t.Fatalf("provider %q has 0 models", pid)
		}
	}

	// 2. Check alias resolution
	aliasTests := map[string]string{
		"oa":   "openai",
		"cc":   "claude",
		"cx":   "codex",
		"cu":   "cursor",
		"kr":   "kiro",
		"ag":   "antigravity",
		"gh":   "github",
		"ds":   "deepseek",
		"gc":   "gemini-cli",
		"gcli": "grok-cli",
		"tr":   "trae",
		"ws":   "windsurf",
		"dv":   "devin-cli",
	}
	for alias, wantID := range aliasTests {
		p := provider.GetProviderByAlias(alias)
		if p == nil || p.ID != wantID {
			t.Fatalf("alias %q resolved to %v, want %q", alias, p, wantID)
		}
	}

	// 3. Verify specific new models from 9router
	codex := provider.GetProvider("codex")
	hasGPT56 := false
	for _, m := range codex.Models {
		if m.ID == "gpt-5.6-sol" || m.ID == "gpt-5.6-terra" {
			hasGPT56 = true
			break
		}
	}
	if !hasGPT56 {
		t.Fatal("codex missing gpt-5.6 models")
	}

	kiro := provider.GetProvider("kiro")
	hasClaude5 := false
	for _, m := range kiro.Models {
		if m.ID == "claude-opus-5" || m.ID == "claude-opus-4.8" {
			hasClaude5 = true
			break
		}
	}
	if !hasClaude5 {
		t.Fatal("kiro missing claude-opus-5 / 4.8 models")
	}
}
