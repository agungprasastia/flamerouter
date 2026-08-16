package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"flamerouter/internal/provider"
)

type connDTO struct {
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	AuthType     string         `json:"authType"`
	Name         string         `json:"name"`
	Priority     int            `json:"priority"`
	IsActive     bool           `json:"isActive"`
	BaseURL      string         `json:"baseUrl,omitempty"`
	TestStatus   string         `json:"testStatus,omitempty"`
	LastError    string         `json:"lastError,omitempty"`
	ExpiresAt    string         `json:"expiresAt,omitempty"`
	SpecificData map[string]any `json:"providerSpecificData,omitempty"`
}

// GET /api/providers — list all connections (9router: {connections: [...]} secrets stripped)
func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	conns, err := s.st.ListAllConnections()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to fetch providers")
		return
	}
	// node name map for compatible providers
	nodeNameMap := map[string]string{}
	if nodes, err := s.st.GetProviderNodes(); err == nil {
		for _, n := range nodes {
			if n.ID != "" && n.Name != "" {
				nodeNameMap[n.ID] = n.Name
			}
		}
	}
	list := make([]map[string]any, 0, len(conns))
	for _, c := range conns {
		name := c.Name
		if name == "" {
			if nn, ok := nodeNameMap[c.Provider]; ok {
				name = nn
			} else if c.ProviderSpecificData != nil {
				if nn, ok := c.ProviderSpecificData["nodeName"].(string); ok && nn != "" {
					name = nn
				}
			}
			if name == "" {
				name = c.Provider
			}
		}
		list = append(list, map[string]any{
			"id": c.ID, "provider": c.Provider, "authType": c.AuthType, "name": name,
			"priority": c.Priority, "isActive": c.IsActive, "baseUrl": c.BaseURL,
			"testStatus": c.TestStatus, "lastError": c.LastError, "expiresAt": c.ExpiresAt,
			"providerSpecificData": c.ProviderSpecificData,
			// secrets omitted: apiKey, accessToken, refreshToken, idToken
		})
	}
	writeJSONOK(w, map[string]any{"connections": list})
}

// GET /api/providers/{id} — list connections for provider id (or single connection if id is connection UUID)
func (s *Server) handleGetProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id required")
		return
	}
	if conn, err := s.st.GetConnection(id); err == nil && conn != nil {
		writeJSONOK(w, map[string]any{"connection": connDTO{
			ID: conn.ID, Provider: conn.Provider, AuthType: conn.AuthType, Name: conn.Name,
			Priority: conn.Priority, IsActive: conn.IsActive, BaseURL: conn.BaseURL,
			TestStatus: conn.TestStatus, LastError: conn.LastError, ExpiresAt: conn.ExpiresAt,
			SpecificData: conn.ProviderSpecificData,
		}})
		return
	}
	conns, err := s.st.ListConnectionsByProvider(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	if len(conns) == 0 && provider.GetProvider(id) == nil {
		writeErr(w, http.StatusNotFound, "connection not found")
		return
	}
	list := make([]connDTO, 0, len(conns))
	for _, c := range conns {
		list = append(list, connDTO{
			ID: c.ID, Provider: c.Provider, AuthType: c.AuthType, Name: c.Name,
			Priority: c.Priority, IsActive: c.IsActive, BaseURL: c.BaseURL,
			TestStatus: c.TestStatus, LastError: c.LastError, ExpiresAt: c.ExpiresAt,
			SpecificData: c.ProviderSpecificData,
		})
	}
	writeJSONOK(w, map[string]any{"connections": list, "provider": id})
}

// PUT /api/providers/{id} — update connection (toggle active, rename, reprioritize)
func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id required")
		return
	}
	conn, err := s.st.GetConnection(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	if conn == nil {
		writeErr(w, http.StatusNotFound, "connection not found")
		return
	}
	var req struct {
		IsActive             *bool          `json:"isActive"`
		Name                 string         `json:"name"`
		Priority             *int           `json:"priority"`
		BaseURL              string         `json:"baseUrl"`
		ProviderSpecificData map[string]any `json:"providerSpecificData"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	isActive := conn.IsActive
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	name := req.Name
	if name == "" {
		name = conn.Name
	}
	priority := conn.Priority
	if req.Priority != nil {
		priority = *req.Priority
	}
	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = conn.BaseURL
	}
	if err := s.st.UpdateConnection(id, isActive, name, priority, baseURL); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	if req.ProviderSpecificData != nil {
		psdJSON, _ := json.Marshal(req.ProviderSpecificData)
		_ = s.st.UpdateConnectionPSD(id, string(psdJSON))
	}
	writeJSONOK(w, map[string]any{"ok": true})
}

// DELETE /api/providers/{id} — delete connection
func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id required")
		return
	}
	if err := s.st.DeleteConnection(id); err != nil {
		if err == sql.ErrNoRows {
			writeErr(w, http.StatusNotFound, "connection not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"ok": true})
}

// POST /api/providers/{id} — create connection for provider
func (s *Server) handleCreateProviderConnection(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	if providerID == "" {
		writeErr(w, http.StatusBadRequest, "provider id required")
		return
	}
	var req struct {
		Name     string `json:"name"`
		APIKey   string `json:"api_key"`
		APIKey2  string `json:"apiKey"`
		BaseURL  string `json:"base_url"`
		Base2    string `json:"baseUrl"`
		AuthType string `json:"auth_type"`
		Auth2    string `json:"authType"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	apiKey := req.APIKey
	if apiKey == "" {
		apiKey = req.APIKey2
	}
	base := req.BaseURL
	if base == "" {
		base = req.Base2
	}
	authType := req.AuthType
	if authType == "" {
		authType = req.Auth2
	}
	if authType == "" {
		authType = "api_key"
	}
	name := req.Name
	if name == "" {
		name = providerID
	}
	id, err := s.st.CreateConnection(providerID, authType, name, apiKey, base)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

// POST /api/providers — create connection from 9router dashboard body {provider, apiKey, name, ...}
func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"apiKey"`
		APIKey2  string `json:"api_key"`
		Name     string `json:"name"`
		Display  string `json:"displayName"`
		AuthType string `json:"authType"`
		AuthType2 string `json:"auth_type"`
		BaseURL  string `json:"baseUrl"`
		Base2    string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	providerID := strings.TrimSpace(req.Provider)
	if providerID == "" {
		writeErr(w, http.StatusBadRequest, "provider required")
		return
	}
	apiKey := req.APIKey
	if apiKey == "" {
		apiKey = req.APIKey2
	}
	base := req.BaseURL
	if base == "" {
		base = req.Base2
	}
	authType := req.AuthType
	if authType == "" {
		authType = req.AuthType2
	}
	if authType == "" {
		authType = "api_key"
	}
	name := req.Name
	if name == "" {
		name = req.Display
	}
	if name == "" {
		if p := provider.GetProvider(providerID); p != nil {
			name = p.Display.Name
		}
	}
	if name == "" {
		name = providerID
	}
	id, err := s.st.CreateConnection(providerID, authType, name, apiKey, base)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"connection": map[string]any{
			"id": id, "provider": providerID, "authType": authType, "name": name,
			"isActive": true, "priority": 0, "testStatus": "unknown",
		},
	})
}

// GET /api/providers/{id}/models — registry models for provider
func (s *Server) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := provider.GetProvider(id)
	if p == nil {
		p = provider.GetProviderByAlias(id)
	}
	if p == nil {
		writeJSONOK(w, map[string]any{"models": []any{}})
		return
	}
	models := make([]map[string]any, 0, len(p.Models))
	for _, m := range p.Models {
		kind := m.Kind
		models = append(models, map[string]any{
			"id":   m.ID,
			"name": m.Name,
			"kind": kind,
		})
	}
	writeJSONOK(w, map[string]any{"models": models})
}

// POST /api/providers/{id}/test — credential presence check (live probe later)
func (s *Server) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	conn, err := s.st.GetConnection(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	if conn == nil {
		conns, _ := s.st.ListActiveByProvider(id)
		if len(conns) == 0 {
			writeErr(w, http.StatusNotFound, "Connection not found")
			return
		}
		conn = &conns[0]
	}
	valid := conn.APIKey != "" || conn.AccessToken != ""
	var errMsg any
	if !valid {
		errMsg = "no credentials"
	}
	writeJSONOK(w, map[string]any{
		"valid":     valid,
		"error":     errMsg,
		"refreshed": false,
	})
}

func (s *Server) handleProviderTestModels(w http.ResponseWriter, r *http.Request) {
	// ponytail: registry list only; live probe later
	s.handleProviderModels(w, r)
}

func (s *Server) handleProviderValidate(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	providerID, _ := body["provider"].(string)
	if providerID == "" {
		providerID, _ = body["id"].(string)
	}
	if providerID == "" {
		writeErr(w, http.StatusBadRequest, "provider required")
		return
	}
	p := provider.GetProvider(providerID)
	if p == nil {
		p = provider.GetProviderByAlias(providerID)
	}
	writeJSONOK(w, map[string]any{
		"valid":    p != nil,
		"provider": providerID,
		"known":    p != nil,
	})
}
