package gateway

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/handlers"
	"flamerouter/internal/opensse/models"
	"flamerouter/internal/provider"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

func (s *Server) handleAllModels(w http.ResponseWriter, r *http.Request) {
	aliases, _ := s.st.ListAliases()
	disabled, _ := s.st.ListDisabledModels()
	disabledSet := map[string]bool{}

	for _, d := range disabled {
		disabledSet[d] = true
	}

	var outModels []map[string]any

	for _, p := range provider.ListProviders() {
		alias := p.Alias
		if alias == "" {
			alias = p.ID
		}

		resolvedDynamic := false

		if s.st != nil {
			if conns, err := s.st.ListActiveByProvider(p.ID); err == nil && len(conns) > 0 {
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				dynModels, dynErr := models.DefaultEngine.ResolveModels(ctx, &conns[0])

				cancel()

				if dynErr == nil && len(dynModels) > 0 {
					resolvedDynamic = true

					for _, dm := range dynModels {
						full := alias + "/" + dm.ID
						if disabledSet[full] || disabledSet[dm.ID] {
							continue
						}

						entry := map[string]any{
							"provider":  p.ID,
							"model":     dm.ID,
							"name":      dm.Name,
							"fullModel": full,
							"alias":     firstNonEmpty(aliases[full], dm.ID),
						}
						outModels = append(outModels, entry)
					}
				}
			}
		}

		if !resolvedDynamic {
			for _, m := range p.Models {
				full := alias + "/" + m.ID
				if disabledSet[full] || disabledSet[m.ID] {
					continue
				}

				entry := map[string]any{
					"provider":  p.ID,
					"model":     m.ID,
					"name":      m.Name,
					"fullModel": full,
					"alias":     firstNonEmpty(aliases[full], m.ID),
				}
				if m.Kind != "" {
					entry["kind"] = m.Kind
				}

				outModels = append(outModels, entry)
			}
		}
	}

	if outModels == nil {
		outModels = []map[string]any{}
	}

	writeJSONOK(w, map[string]any{"models": outModels})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

func (s *Server) handleTestModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
		Kind  string `json:"kind"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Model) == "" {
		writeErr(w, http.StatusBadRequest, "Model required")
		return
	}

	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "llm"
	}

	start := time.Now()

	var probeBody []byte
	switch kind {
	case "embedding":
		probeBody, _ = json.Marshal(map[string]any{
			"model": req.Model,
			"input": "test",
		})
	case "image":
		probeBody, _ = json.Marshal(map[string]any{
			"model":  req.Model,
			"prompt": "test",
		})
	default:
		probeBody, _ = json.Marshal(map[string]any{
			"model":      req.Model,
			"max_tokens": 1024,
			"stream":     false,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		})
	}

	rec := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	var err error

	switch kind {
	case "embedding":
		err = handlers.Embeddings(ctx, rec, probeBody, s.st, s.exec, s.fb)
	case "image":
		err = handlers.ImageGeneration(ctx, rec, probeBody, s.st, s.exec, s.fb)
	default:
		ts := handlers.LoadTokenSaverFromStore(s.st)
		err = handlers.ChatWithOptions(ctx, rec, probeBody, s.st, s.exec, s.fb, handlers.ChatOptions{
			TokenSaver: ts,
			Usage:      usageBridge{s.tracker},
		})
	}

	latencyMs := time.Since(start).Milliseconds()

	statusCode := rec.Code
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	if err != nil {
		writeJSONOK(w, map[string]any{
			"ok":        false,
			"model":     req.Model,
			"kind":      kind,
			"error":     err.Error(),
			"latencyMs": latencyMs,
			"status":    statusCode,
		})

		return
	}

	if statusCode >= 400 {
		raw := strings.TrimSpace(rec.Body.String())
		errMsg := fmt.Sprintf("HTTP %d", statusCode)

		if raw != "" {
			errMsg = fmt.Sprintf("HTTP %d: %s", statusCode, raw)
		}

		writeJSONOK(w, map[string]any{
			"ok":        false,
			"model":     req.Model,
			"kind":      kind,
			"error":     errMsg,
			"latencyMs": latencyMs,
			"status":    statusCode,
		})

		return
	}

	writeJSONOK(w, map[string]any{
		"ok":        true,
		"model":     req.Model,
		"kind":      kind,
		"latencyMs": latencyMs,
		"status":    statusCode,
	})
}

func (s *Server) handleListDisabledModels(w http.ResponseWriter, r *http.Request) {
	list, err := s.st.ListDisabledModels()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	if list == nil {
		list = []string{}
	}

	writeJSONOK(w, map[string]any{"disabled": list})
}

func (s *Server) handleToggleDisabledModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model   string `json:"model"`
		Disable *bool  `json:"disable"`
		// 9router POST body: providerAlias + ids
		ProviderAlias string   `json:"providerAlias"`
		IDs           []string `json:"ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if len(req.IDs) > 0 && req.ProviderAlias != "" {
		for _, id := range req.IDs {
			_ = s.st.DisableModel(req.ProviderAlias + "/" + id)
		}

		writeJSONOK(w, map[string]any{"success": true})

		return
	}

	if req.Model == "" {
		writeErr(w, http.StatusBadRequest, "model required")
		return
	}

	disable := true
	if req.Disable != nil {
		disable = *req.Disable
	}

	var err error
	if disable {
		err = s.st.DisableModel(req.Model)
	} else {
		err = s.st.EnableModel(req.Model)
	}

	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, map[string]any{"success": true})
}

func (s *Server) handleListAliases(w http.ResponseWriter, r *http.Request) {
	aliases, err := s.st.ListAliases()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	if aliases == nil {
		aliases = map[string]string{}
	}

	writeJSONOK(w, map[string]any{"aliases": aliases})
}

func (s *Server) handleModelAvailability(w http.ResponseWriter, r *http.Request) {
	// ponytail: no model-lock columns yet
	writeJSONOK(w, map[string]any{"models": []any{}})
}
