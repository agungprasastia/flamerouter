package gateway

import (
	"encoding/json"
	"flamerouter/internal/clitools"
	"net/http"
	"strings"
)

func (s *Server) cliTools() *clitools.Manager {
	return clitools.New(s.st)
}

func (s *Server) getRequestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:20130"
	}
	return scheme + "://" + host
}

// GET/POST/PATCH/DELETE /api/cli-tools/{toolSettings}
func (s *Server) handleCLIToolSettings(w http.ResponseWriter, r *http.Request) {
	seg := r.PathValue("toolSettings")

	toolID := strings.TrimSuffix(seg, "-settings")
	if toolID == "" {
		writeErr(w, http.StatusBadRequest, "invalid tool settings path")
		return
	}

	m := s.cliTools()
	baseURL := s.getRequestBaseURL(r)

	switch r.Method {
	case http.MethodGet:
		status, err := m.GetStatus(toolID, baseURL)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONOK(w, status)

	case http.MethodPost, http.MethodPatch:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		if _, ok := body["baseUrl"]; !ok || body["baseUrl"] == "" {
			body["baseUrl"] = baseURL
		}

		res, err := m.ApplySettings(toolID, body)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONOK(w, res)

	case http.MethodDelete:
		res, err := m.ResetSettings(toolID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONOK(w, res)

	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GET /api/cli-tools/all-statuses.
func (s *Server) handleAllCLIStatuses(w http.ResponseWriter, r *http.Request) {
	baseURL := s.getRequestBaseURL(r)
	writeJSONOK(w, s.cliTools().AllStatuses(baseURL))
}

