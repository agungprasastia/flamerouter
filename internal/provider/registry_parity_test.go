package provider_test

import (
	"flamerouter/internal/provider"
	"testing"
)

func verifyExpectedProviders(t *testing.T) {
	t.Helper()

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
}

func verifyAliases(t *testing.T) {
	t.Helper()

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
}

func checkCodexModels(t *testing.T) {
	t.Helper()

	codex := provider.GetProvider("codex")
	if codex == nil {
		t.Fatal("codex provider not found")
	}

	for _, m := range codex.Models {
		if m.ID == "gpt-5.6-sol" || m.ID == "gpt-5.6-terra" {
			return
		}
	}

	t.Fatal("codex missing gpt-5.6 models")
}

func checkKiroModels(t *testing.T) {
	t.Helper()

	kiro := provider.GetProvider("kiro")
	if kiro == nil {
		t.Fatal("kiro provider not found")
	}

	for _, m := range kiro.Models {
		if m.ID == "claude-opus-5" || m.ID == "claude-opus-4.8" {
			return
		}
	}

	t.Fatal("kiro missing claude-opus-5 / 4.8 models")
}

func TestProviderRegistryModelsParity(t *testing.T) {
	verifyExpectedProviders(t)
	verifyAliases(t)
	checkCodexModels(t)
	checkKiroModels(t)
}
