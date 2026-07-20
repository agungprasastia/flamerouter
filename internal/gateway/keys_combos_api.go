package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

func (s *Server) handleUpdateKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		IsActive *bool `json:"isActive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.IsActive == nil {
		writeErr(w, http.StatusBadRequest, "isActive required")
		return
	}
	err := s.st.UpdateAPIKey(id, *req.IsActive)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "Key not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"success": true, "id": id, "isActive": *req.IsActive})
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.st.DeleteAPIKey(id)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "Key not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"message": "Key deleted successfully"})
}

func (s *Server) handleUpdateCombo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name   string   `json:"name"`
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "name required")
		return
	}
	if req.Models == nil {
		req.Models = []string{}
	}
	err := s.st.UpdateCombo(id, req.Name, req.Models)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "Combo not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"id": id, "name": req.Name, "models": req.Models})
}

func (s *Server) handleDeleteCombo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.st.DeleteCombo(id)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "Combo not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"success": true})
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	// 9router returns ollama model tags list; minimal empty ok
	writeJSONOK(w, []any{})
}

func (s *Server) handleLocale(w http.ResponseWriter, r *http.Request) {
	writeJSONOK(w, map[string]any{"locale": "en"})
}

func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Initialized"))
}
