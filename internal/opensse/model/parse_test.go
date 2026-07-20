package model_test

import (
	"testing"

	"flamerouter/internal/opensse/model"
)

func TestParseModel_ProviderSlash(t *testing.T) {
	m := model.ParseModel("openai/gpt-4o")
	if m.Provider != "openai" || m.Model != "gpt-4o" || m.IsAlias {
		t.Fatalf("%+v", m)
	}
}

func TestParseModel_NestedModelName(t *testing.T) {
	m := model.ParseModel("openrouter/meta/llama-3")
	if m.Provider != "openrouter" || m.Model != "meta/llama-3" {
		t.Fatalf("%+v", m)
	}
}

func TestParseModel_BareAlias(t *testing.T) {
	m := model.ParseModel("my-alias")
	if !m.IsAlias || m.Model != "my-alias" || m.Provider != "" {
		t.Fatalf("%+v", m)
	}
}

func TestResolveProviderAlias(t *testing.T) {
	aliases := map[string]string{"or": "openrouter", "openrouter": "openrouter"}
	if model.ResolveProviderAlias("or", aliases) != "openrouter" {
		t.Fatal()
	}
	if model.ResolveProviderAlias("unknown", aliases) != "unknown" {
		t.Fatal()
	}
}
