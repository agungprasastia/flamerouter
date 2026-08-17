package handlers

import (
	"flamerouter/internal/provider"
	"strings"
)

func lookupModelInfo(fullID, requestedKind string) map[string]any {
	alias, modelID, ok := splitModelID(fullID)
	if !ok {
		return nil
	}

	p := findProvider(alias)
	if p == nil {
		return nil
	}

	outAlias := p.Alias
	if outAlias == "" {
		outAlias = p.ID
	}

	for _, m := range p.Models {
		if m.ID != modelID {
			continue
		}

		mk := modelKind(m)
		if !matchesRequestedKind(mk, requestedKind) {
			continue
		}

		return buildModelInfoMap(m, outAlias, mk)
	}

	return nil
}

func splitModelID(fullID string) (alias, modelID string, ok bool) {
	slash := strings.Index(fullID, "/")
	if slash < 0 {
		return "", "", false
	}

	return fullID[:slash], fullID[slash+1:], true
}

func findProvider(alias string) *provider.Provider {
	p := provider.GetProviderByAlias(alias)
	if p == nil {
		p = provider.GetProvider(alias)
	}

	return p
}

func matchesRequestedKind(mk, requestedKind string) bool {
	if requestedKind == "" || mk == requestedKind {
		return true
	}

	return requestedKind == "image-to-text" && mk == "imageToText"
}

func buildModelInfoMap(m provider.Model, outAlias, mk string) map[string]any {
	info := map[string]any{
		"id":           outAlias + "/" + m.ID,
		"name":         m.Name,
		"kind":         mk,
		"owned_by":     outAlias,
		"endpoint":     kindEndpoint[mk],
		"capabilities": provider.GetCapabilities(m.ID),
	}
	if m.Params != nil {
		info["params"] = m.Params
	}

	if m.Dimensions > 0 {
		info["dimensions"] = m.Dimensions
	}

	return info
}
