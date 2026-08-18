package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
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

func (s *Server) handleGetCombo(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	combo, err := s.st.GetComboByID(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	if combo == nil {
		writeErr(w, http.StatusNotFound, "Combo not found")
		return
	}

	writeJSONOK(w, combo)
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

func (s *Server) handleTags(w http.ResponseWriter, _ *http.Request) {
	// 9router returns ollama model tags list; minimal empty ok
	writeJSONOK(w, []any{})
}

func (s *Server) handleLocale(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var body struct {
			Locale string `json:"locale"`
		}

		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Locale == "" {
			writeErr(w, http.StatusBadRequest, "Invalid locale")
			return
		}
		// accept any short locale tag; store cookie for dashboard
		loc := body.Locale
		if len(loc) > 16 {
			loc = loc[:16]
		}

		http.SetCookie(w, &http.Cookie{
			Name:        "locale",
			Value:       loc,
			Path:        "/",
			Domain:      "",
			Expires:     time.Time{},
			RawExpires:  "",
			MaxAge:      60 * 60 * 24 * 365,
			Secure:      false,
			HttpOnly:    false,
			SameSite:    http.SameSiteLaxMode,
			Raw:         "",
			Unparsed:    nil,
			Partitioned: false,
			Quoted:      false,
		})

		if err := s.st.SetSetting("locale", loc); err != nil {
			_ = err
		}

		writeJSONOK(w, map[string]any{"success": true, "locale": loc})

		return
	}

	loc, err := s.st.GetSetting("locale")
	if err != nil || loc == "" {
		if c, errCookie := r.Cookie("locale"); errCookie == nil && c.Value != "" {
			loc = c.Value
		} else {
			loc = "en"
		}
	}

	writeJSONOK(w, map[string]any{"locale": loc})
}

func (s *Server) handleInit(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte("Initialized")); err != nil {
		_ = err
	}
}
