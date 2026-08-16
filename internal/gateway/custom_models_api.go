package gateway

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleListCustomModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.st.ListCustomModels()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	list := make([]map[string]any, 0, len(models))

	for _, m := range models {
		displayName := m.DisplayName
		if displayName == "" {
			displayName = m.ModelID
		}

		list = append(list, map[string]any{
			"id":            m.ModelID,
			"model_id":      m.ModelID,
			"customId":      m.ID,
			"provider":      m.Provider,
			"providerAlias": m.Provider,
			"name":          displayName,
			"display_name":  displayName,
			"type":          "llm",
			"fullModel":     m.Provider + "/" + m.ModelID,
			"capabilities":  m.Capabilities,
		})
	}

	writeJSONOK(w, map[string]any{"models": list})
}

func (s *Server) handleCreateCustomModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider      string `json:"provider"`
		ProviderAlias string `json:"providerAlias"`
		ModelID       string `json:"model_id"`
		ID            string `json:"id"`
		DisplayName   string `json:"display_name"`
		Name          string `json:"name"`
		Type          string `json:"type"`
		Capabilities  string `json:"capabilities"`
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

	if name == "" {
		name = mid
	}

	id, err := s.st.CreateCustomModel(prov, mid, name, req.Capabilities)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"id":      id,
		"added": map[string]any{
			"id":            mid,
			"providerAlias": prov,
			"name":          name,
			"type":          req.Type,
		},
	})
}
