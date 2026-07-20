package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"flamerouter/internal/provider"
	"flamerouter/internal/store"
)

const llmKind = "llm"

var kindSlugMap = map[string][]string{
	"image":         {"image"},
	"tts":           {"tts"},
	"stt":           {"stt"},
	"embedding":     {"embedding"},
	"image-to-text": {"imageToText"},
	"web":           {"webSearch", "webFetch"},
	"video":         {"video"},
	"llm":           {llmKind},
}

var kindEndpoint = map[string]string{
	"llm":         "/v1/chat/completions",
	"image":       "/v1/images/generations",
	"tts":         "/v1/audio/speech",
	"stt":         "/v1/audio/transcriptions",
	"embedding":   "/v1/embeddings",
	"imageToText": "/v1/chat/completions",
	"webSearch":   "/v1/search",
	"webFetch":    "/v1/web/fetch",
	"video":       "/v1/videos/generations",
}

// Models handles GET /v1/models — LLM/chat models by default.
func Models(w http.ResponseWriter, r *http.Request, st *store.Store) {
	writeModelList(w, st, []string{llmKind}, false)
}

// ModelsByKind handles GET /v1/models/{kind}.
func ModelsByKind(w http.ResponseWriter, r *http.Request, st *store.Store, kind string) {
	filter, ok := kindSlugMap[kind]
	if !ok {
		jsonError(w, http.StatusNotFound, "Unknown model kind: "+kind+". Supported: image, tts, stt, embedding, image-to-text, web, video")
		return
	}
	writeModelList(w, st, filter, false)
}

// ModelsInfo handles GET /v1/models/info?id=alias/model.
func ModelsInfo(w http.ResponseWriter, r *http.Request, st *store.Store) {
	id := r.URL.Query().Get("id")
	if id == "" {
		// brief: list with capabilities when no id
		writeModelList(w, st, []string{llmKind}, true)
		return
	}
	kindQ := r.URL.Query().Get("kind")
	info := lookupModelInfo(id, kindQ)
	if info == nil {
		jsonError(w, http.StatusNotFound, "Model not found: "+id)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
	_ = st
}

func writeModelList(w http.ResponseWriter, st *store.Store, kindFilter []string, withCaps bool) {
	data := buildModelList(st, kindFilter, withCaps)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	})
}

func buildModelList(st *store.Store, kindFilter []string, withCaps bool) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}

	// combos first (owned_by combo) — always llm
	if kindIn(kindFilter, llmKind) && st != nil {
		if combos, err := st.ListCombos(); err == nil {
			for _, c := range combos {
				if seen[c.Name] {
					continue
				}
				seen[c.Name] = true
				out = append(out, map[string]any{
					"id":       c.Name,
					"object":   "model",
					"owned_by": "combo",
				})
			}
		}
	}

	activeProviders := activeProviderSet(st)
	for _, p := range provider.ListProviders() {
		if !providerMatchesKinds(p, kindFilter) {
			continue
		}
		// if any connections exist, only providers with active conn
		if len(activeProviders) > 0 && !activeProviders[p.ID] {
			continue
		}
		alias := p.Alias
		if alias == "" {
			alias = p.ID
		}
		for _, m := range p.Models {
			mk := modelKind(m)
			if !kindIn(kindFilter, mk) && !(mk == "imageToText" && kindIn(kindFilter, llmKind)) {
				continue
			}
			id := alias + "/" + m.ID
			if seen[id] {
				continue
			}
			seen[id] = true
			entry := map[string]any{
				"id":       id,
				"object":   "model",
				"owned_by": alias,
			}
			if withCaps || mk == llmKind {
				caps := provider.GetCapabilities(m.ID)
				entry["capabilities"] = caps
			}
			out = append(out, entry)
		}
	}
	if out == nil {
		out = []map[string]any{}
	}
	return out
}

func activeProviderSet(st *store.Store) map[string]bool {
	set := map[string]bool{}
	if st == nil {
		return set
	}
	for _, p := range provider.ListProviders() {
		conns, err := st.ListActiveByProvider(p.ID)
		if err == nil && len(conns) > 0 {
			set[p.ID] = true
		}
	}
	return set
}

func providerMatchesKinds(p provider.Provider, kindFilter []string) bool {
	kinds := p.ServiceKinds
	if len(kinds) == 0 {
		kinds = []string{llmKind}
	}
	for _, k := range kinds {
		if kindIn(kindFilter, k) {
			return true
		}
	}
	// also match if any model kind matches
	for _, m := range p.Models {
		if kindIn(kindFilter, modelKind(m)) {
			return true
		}
	}
	return false
}

func modelKind(m provider.Model) string {
	if m.Kind == "" {
		return llmKind
	}
	switch m.Kind {
	case "imageToText", "image-to-text":
		return "imageToText"
	default:
		return m.Kind
	}
}

func kindIn(filter []string, k string) bool {
	for _, f := range filter {
		if f == k {
			return true
		}
	}
	return false
}

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
