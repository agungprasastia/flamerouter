// Package model provides model identification and aliasing helpers.
package model

import "strings"

// Ref represents a parsed model identifier.
type Ref struct {
	Provider      string
	Model         string
	ProviderAlias string
	IsAlias       bool
}

// ParseModel parses a raw model string into a Ref.
func ParseModel(modelStr string) Ref {
	if modelStr == "" {
		return Ref{
			Provider:      "",
			Model:         "",
			ProviderAlias: "",
			IsAlias:       false,
		}
	}

	if i := strings.Index(modelStr, "/"); i >= 0 {
		pa := modelStr[:i]

		return Ref{
			Provider:      pa,
			Model:         modelStr[i+1:],
			ProviderAlias: pa,
			IsAlias:       false,
		}
	}

	return Ref{
		Provider:      "",
		Model:         modelStr,
		ProviderAlias: "",
		IsAlias:       true,
	}
}

// ResolveProviderAlias returns the resolved provider ID from aliases.
func ResolveProviderAlias(aliasOrID string, aliases map[string]string) string {
	if aliases != nil {
		if id, ok := aliases[aliasOrID]; ok {
			return id
		}
	}

	return aliasOrID
}

// ResolveModelAlias resolves a model alias to its target Ref.
func ResolveModelAlias(alias string, aliases map[string]string) (Ref, bool) {
	if aliases == nil {
		return Ref{
			Provider:      "",
			Model:         "",
			ProviderAlias: "",
			IsAlias:       false,
		}, false
	}

	target, ok := aliases[alias]
	if !ok {
		return Ref{
			Provider:      "",
			Model:         "",
			ProviderAlias: "",
			IsAlias:       false,
		}, false
	}

	m := ParseModel(target)
	m.IsAlias = false

	return m, true
}
