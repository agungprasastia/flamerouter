package model

import "strings"

type ModelRef struct {
	Provider      string
	Model         string
	ProviderAlias string
	IsAlias       bool
}

func ParseModel(modelStr string) ModelRef {
	if modelStr == "" {
		return ModelRef{}
	}

	if i := strings.Index(modelStr, "/"); i >= 0 {
		pa := modelStr[:i]

		return ModelRef{
			Provider:      pa,
			Model:         modelStr[i+1:],
			ProviderAlias: pa,
			IsAlias:       false,
		}
	}

	return ModelRef{Model: modelStr, IsAlias: true}
}

func ResolveProviderAlias(aliasOrID string, aliases map[string]string) string {
	if aliases != nil {
		if id, ok := aliases[aliasOrID]; ok {
			return id
		}
	}

	return aliasOrID
}

func ResolveModelAlias(alias string, aliases map[string]string) (ModelRef, bool) {
	if aliases == nil {
		return ModelRef{}, false
	}

	target, ok := aliases[alias]
	if !ok {
		return ModelRef{}, false
	}

	m := ParseModel(target)
	m.IsAlias = false

	return m, true
}
