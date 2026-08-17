// Package provider defines model provider configurations, registry listings,
// and model capabilities for all supported AI backends.
package provider

import (
	_ "embed"
	"encoding/json"
)

//go:embed registry.json
var registryJSON []byte

// Registry contains the catalog of all statically configured providers and their models.
var Registry []Provider

var (
	providerMap      = make(map[string]*Provider)
	providerAliasMap = make(map[string]string)
)

func init() {
	if err := json.Unmarshal(registryJSON, &Registry); err != nil {
		panic("failed to unmarshal embedded provider registry: " + err.Error())
	}

	for i := range Registry {
		p := &Registry[i]
		providerMap[p.ID] = p

		if p.Alias != "" {
			providerAliasMap[p.Alias] = p.ID
		}
	}

	initExtraAliases()
}

func initExtraAliases() {
	extra := map[string]string{
		"oa":        "openai",
		"cc":        "claude",
		"ag":        "antigravity",
		"cx":        "codex",
		"cu":        "cursor",
		"kr":        "kiro",
		"gh":        "github",
		"ds":        "deepseek",
		"if":        "iflow",
		"gc":        "gemini-cli",
		"gcli":      "grok-cli",
		"cl":        "cline",
		"oc":        "opencode",
		"qd":        "qoder",
		"cbcn":      "codebuddy-cn",
		"cbai":      "codebuddy",
		"zd":        "zed",
		"or":        "openrouter",
		"qw":        "alicode",
		"qwen":      "alicode",
		"dashscope": "alicode",
		"tg":        "together",
		"fw":        "fireworks",
		"cb":        "cerebras",
		"co":        "cohere",
		"ol":        "ollama",
		"az":        "azure",
		"mi":        "mistral",
		"tr":        "trae",
		"ws":        "windsurf",
		"dv":        "devin-cli",
	}
	for k, v := range extra {
		providerAliasMap[k] = v
	}
}

// GetProvider retrieves a provider by exact identifier.
func GetProvider(id string) *Provider {
	if p, ok := providerMap[id]; ok {
		return p
	}

	return nil
}

// GetProviderByAlias retrieves a provider by alias or ID.
func GetProviderByAlias(alias string) *Provider {
	if id, ok := providerAliasMap[alias]; ok {
		return GetProvider(id)
	}

	for _, p := range Registry {
		if p.Alias == alias {
			return &p
		}
	}

	return nil
}

// ResolveAlias returns the canonical provider ID for a given alias or returns the input.
func ResolveAlias(aliasOrID string) string {
	if id, ok := providerAliasMap[aliasOrID]; ok {
		return id
	}

	if p := GetProvider(aliasOrID); p != nil {
		return p.ID
	}

	return aliasOrID
}

// Aliases returns a copy of all registered provider aliases.
func Aliases() map[string]string {
	res := make(map[string]string, len(providerAliasMap))
	for k, v := range providerAliasMap {
		res[k] = v
	}

	return res
}

// ListProviders returns all registered providers.
func ListProviders() []Provider {
	return Registry
}
