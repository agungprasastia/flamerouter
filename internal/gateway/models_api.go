package gateway

import (
	"encoding/json"
	"net/http"
	"strings"

	"flamerouter/internal/provider"
)

func (s *Server) handleAllModels(w http.ResponseWriter, r *http.Request) {
	aliases, _ := s.st.ListAliases()
	disabled, _ := s.st.ListDisabledModels()
	disabledSet := map[string]bool{}
	for _, d := range disabled {
		disabledSet[d] = true
	}
	var models []map[string]any
	for _, p := range provider.ListProviders() {
		alias := p.Alias
		if alias == "" {
			alias = p.ID
		}
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
			models = append(models, entry)
		}
	}
	if models == nil {
		models = []map[string]any{}
	}
	writeJSONOK(w, map[string]any{"models": models})
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
	// ponytail: no live ping; registry presence only
	ok := false
	for _, p := range provider.ListProviders() {
		alias := p.Alias
		if alias == "" {
			alias = p.ID
		}
		for _, m := range p.Models {
			if m.ID == req.Model || alias+"/"+m.ID == req.Model || p.ID+"/"+m.ID == req.Model {
				ok = true
				break
			}
		}
		if ok {
			break
		}
	}
	writeJSONOK(w, map[string]any{"ok": ok, "model": req.Model, "kind": req.Kind})
}

func (s *Server) handleListCustomModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.st.ListCustomModels()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	list := make([]map[string]any, 0, len(models))
	for _, m := range models {
		list = append(list, map[string]any{
			"id": m.ID, "provider": m.Provider, "model_id": m.ModelID,
			"display_name": m.DisplayName, "capabilities": m.Capabilities,
		})
	}
	writeJSONOK(w, map[string]any{"models": list})
}

func (s *Server) handleCreateCustomModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider     string `json:"provider"`
		ProviderAlias string `json:"providerAlias"`
		ModelID      string `json:"model_id"`
		ID           string `json:"id"`
		DisplayName  string `json:"display_name"`
		Name         string `json:"name"`
		Capabilities string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	prov := req.Provider
	if prov == "" {
		prov = req.ProviderAlias
	}
	mid := req.ModelID
	if mid == "" {
		mid = req.ID
	}
	if prov == "" || mid == "" {
		writeErr(w, http.StatusBadRequest, "provider and model id required")
		return
	}
	name := req.DisplayName
	if name == "" {
		name = req.Name
	}
	id, err := s.st.CreateCustomModel(prov, mid, name, req.Capabilities)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "id": id})
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
