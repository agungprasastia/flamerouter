package gateway

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flamerouter/internal/store"
	"net/http"
	"strings"
)

func (s *Server) handleListProviderNodes(w http.ResponseWriter, _ *http.Request) {
	nodes, err := s.st.GetProviderNodes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	if nodes == nil {
		nodes = []store.ProviderNode{}
	}

	list := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		list = append(list, map[string]any{
			"id": n.ID, "type": n.Type, "name": n.Name,
			"prefix": n.Prefix, "apiType": n.APIType, "baseUrl": n.BaseURL,
		})
	}

	writeJSONOK(w, map[string]any{"nodes": list})
}

type providerNodeCreateReq struct {
	Name    string `json:"name"`
	Prefix  string `json:"prefix"`
	APIType string `json:"apiType"`
	BaseURL string `json:"baseUrl"`
	Type    string `json:"type"`
}

func normalizeNodeBaseURL(req *providerNodeCreateReq) {
	req.BaseURL = strings.TrimRight(req.BaseURL, "/")
	if req.Type == "anthropic-compatible" && strings.HasSuffix(req.BaseURL, "/messages") {
		req.BaseURL = strings.TrimSuffix(req.BaseURL, "/messages")
	}

	if req.Type == "custom-embedding" && strings.HasSuffix(req.BaseURL, "/embeddings") {
		req.BaseURL = strings.TrimSuffix(req.BaseURL, "/embeddings")
	}
}

func sanitizeNodeCreateReq(req *providerNodeCreateReq) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Prefix = strings.TrimSpace(req.Prefix)
	req.BaseURL = strings.TrimSpace(req.BaseURL)

	if req.Name == "" {
		return errors.New("name is required")
	}

	if req.Prefix == "" {
		return errors.New("prefix is required")
	}

	if req.Type == "" {
		req.Type = "openai-compatible"
	}

	if req.BaseURL == "" {
		req.BaseURL = "https://api.openai.com/v1"
	}

	normalizeNodeBaseURL(req)

	if req.Type == "openai-compatible" && req.APIType != "chat" && req.APIType != "responses" {
		if req.APIType == "" {
			req.APIType = "chat"
		} else {
			return errors.New("invalid openai compatible api type")
		}
	}

	return nil
}

func (s *Server) handleCreateProviderNode(w http.ResponseWriter, r *http.Request) {
	var req providerNodeCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if err := sanitizeNodeCreateReq(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := s.st.CreateProviderNode(req.Type, req.Name, req.Prefix, req.APIType, req.BaseURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"node": map[string]any{
			"id": id, "type": req.Type, "name": req.Name,
			"prefix": req.Prefix, "apiType": req.APIType, "baseUrl": req.BaseURL,
		},
	})
}

func (s *Server) handleUpdateProviderNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		Name    string `json:"name"`
		Prefix  string `json:"prefix"`
		APIType string `json:"apiType"`
		BaseURL string `json:"baseUrl"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Prefix = strings.TrimSpace(req.Prefix)
	req.BaseURL = strings.TrimSpace(req.BaseURL)

	if req.Name == "" || req.Prefix == "" || req.BaseURL == "" {
		writeErr(w, http.StatusBadRequest, "name, prefix, baseUrl required")
		return
	}

	err := s.st.UpdateProviderNode(id, req.Name, req.Prefix, req.APIType, req.BaseURL)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "Provider node not found")
		return
	}

	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, map[string]any{
		"node": map[string]any{
			"id": id, "name": req.Name, "prefix": req.Prefix,
			"apiType": req.APIType, "baseUrl": req.BaseURL,
		},
	})
}

func (s *Server) handleDeleteProviderNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	err := s.st.DeleteProviderNode(id)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "Provider node not found")
		return
	}

	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, map[string]any{"success": true})
}

func (s *Server) handleValidateProviderNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		Prefix string `json:"prefix"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Prefix) == "" {
		writeJSONOK(w, map[string]any{"valid": false, "error": "name and prefix required"})
		return
	}

	writeJSONOK(w, map[string]any{"valid": true})
}
