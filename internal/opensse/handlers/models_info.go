package handlers

import (
	"flamerouter/internal/provider"
	"strings"
)

func lookupModelInfo(fullID, requestedKind string) map[string]any {
	slash := strings.Index(fullID, "/")
	if slash < 0 {
		return nil
	}

	alias := fullID[:slash]
	modelID := fullID[slash+1:]

	p := provider.GetProviderByAlias(alias)
	if p == nil {
		p = provider.GetProvider(alias)
	}

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
		if requestedKind != "" && mk != requestedKind && requestedKind != mk {
			// allow slug forms
			if !(requestedKind == "image-to-text" && mk == "imageToText") {
				continue
			}
		}

		info := map[string]any{
			"id":       outAlias + "/" + m.ID,
			"name":     m.Name,
			"kind":     mk,
			"owned_by": outAlias,
			"endpoint": kindEndpoint[mk],
		}
		if m.Params != nil {
			info["params"] = m.Params
		}

		if m.Dimensions > 0 {
			info["dimensions"] = m.Dimensions
		}

		caps := provider.GetCapabilities(m.ID)
		info["capabilities"] = caps

		return info
	}

	return nil
}
