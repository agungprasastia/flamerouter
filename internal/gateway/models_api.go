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

func (s *Server) resolveDynamicModels(ctx context.Context, p provider.Provider, alias string, disabledSet map[string]bool, aliases map[string]string) ([]map[string]any, bool) {
	if s.st == nil {
		return nil, false
	}

	conns, err := s.st.ListActiveByProvider(p.ID)
	if err != nil || len(conns) == 0 {
		return nil, false
	}

	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	dynModels, dynErr := models.DefaultEngine.ResolveModels(subCtx, &conns[0])

	cancel()

	if dynErr != nil || len(dynModels) == 0 {
		return nil, false
	}

	out := make([]map[string]any, 0, len(dynModels))

	for _, dm := range dynModels {
		full := alias + "/" + dm.ID
		if disabledSet[full] || disabledSet[dm.ID] {
			continue
		}

		out = append(out, map[string]any{
			"provider":  p.ID,
			"model":     dm.ID,
			"name":      dm.Name,
			"fullModel": full,
			"alias":     firstNonEmpty(aliases[full], dm.ID),
		})
	}

	return out, true
}

func (s *Server) resolveStaticModels(p provider.Provider, alias string, disabledSet map[string]bool, aliases map[string]string) []map[string]any {
	out := make([]map[string]any, 0, len(p.Models))

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

		out = append(out, entry)
	}

	return out
}

func (s *Server) resolveProviderModels(ctx context.Context, p provider.Provider, alias string, disabledSet map[string]bool, aliases map[string]string) []map[string]any {
	if dyn, ok := s.resolveDynamicModels(ctx, p, alias, disabledSet, aliases); ok {
		return dyn
	}

	return s.resolveStaticModels(p, alias, disabledSet, aliases)
}

func (s *Server) handleAllModels(w http.ResponseWriter, r *http.Request) {
	aliases, err := s.st.ListAliases()
	if err != nil {
		aliases = map[string]string{}
	}

	disabled, err := s.st.ListDisabledModels()
	if err != nil {
		disabled = []string{}
	}

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

		entries := s.resolveProviderModels(r.Context(), p, alias, disabledSet, aliases)
		outModels = append(outModels, entries...)
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

func buildProbeBody(kind, model string) []byte {
	var probeBody []byte

	switch kind {
	case "embedding":
		probeBody, _ = json.Marshal(map[string]any{ //nolint:errcheck // deterministic struct
			"model": model,
			"input": "test",
		})
	case "image":
		probeBody, _ = json.Marshal(map[string]any{ //nolint:errcheck // deterministic struct
			"model":  model,
			"prompt": "test",
		})
	default:
		probeBody, _ = json.Marshal(map[string]any{ //nolint:errcheck // deterministic struct
			"model":      model,
			"max_tokens": 1024,
			"stream":     false,
			"messages": []map[string]string{
				{"role": "user", "content": "hi"},
			},
		})
	}

	return probeBody
}

func (s *Server) executeModelProbe(ctx context.Context, kind string, probeBody []byte, rec http.ResponseWriter) error {
	switch kind {
	case "embedding":
		return handlers.Embeddings(ctx, rec, probeBody, s.st, s.exec, s.fb)
	case "image":
		return handlers.ImageGeneration(ctx, rec, probeBody, s.st, s.exec, s.fb)
	default:
		ts := handlers.LoadTokenSaverFromStore(s.st)

		return handlers.ChatWithOptions(ctx, rec, probeBody, s.st, s.exec, s.fb, handlers.ChatOptions{
			ClientHeaders:   nil,
			SourceFormat:    "",
			AccountStrategy: "",
			StickyLimit:     0,
			TokenSaver:      ts,
			Usage:           usageBridge{t: s.tracker},
		})
	}
}

func (s *Server) runProbeAndFormatResult(ctx context.Context, kind, model string, probeBody []byte) map[string]any {
	start := time.Now()
	rec := httptest.NewRecorder()

	err := s.executeModelProbe(ctx, kind, probeBody, rec)
	latencyMs := time.Since(start).Milliseconds()

	statusCode := rec.Code
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	if err != nil {
		return map[string]any{
			"ok":        false,
			"model":     model,
			"kind":      kind,
			"error":     err.Error(),
			"latencyMs": latencyMs,
			"status":    statusCode,
		}
	}

	if statusCode >= 400 {
		raw := strings.TrimSpace(rec.Body.String())
		errMsg := fmt.Sprintf("HTTP %d", statusCode)

		if raw != "" {
			errMsg = fmt.Sprintf("HTTP %d: %s", statusCode, raw)
		}

		return map[string]any{
			"ok":        false,
			"model":     model,
			"kind":      kind,
			"error":     errMsg,
			"latencyMs": latencyMs,
			"status":    statusCode,
		}
	}

	return map[string]any{
		"ok":        true,
		"model":     model,
		"kind":      kind,
		"latencyMs": latencyMs,
		"status":    statusCode,
	}
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

	probeBody := buildProbeBody(kind, req.Model)

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	res := s.runProbeAndFormatResult(ctx, kind, req.Model, probeBody)
	writeJSONOK(w, res)
}

func (s *Server) handleListDisabledModels(w http.ResponseWriter, _ *http.Request) {
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
			if err := s.st.DisableModel(req.ProviderAlias + "/" + id); err != nil {
				_ = err
			}
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

func (s *Server) handleListAliases(w http.ResponseWriter, _ *http.Request) {
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

func (s *Server) handleModelAvailability(w http.ResponseWriter, _ *http.Request) {
	// ponytail: no model-lock columns yet
	writeJSONOK(w, map[string]any{"models": []any{}})
}
