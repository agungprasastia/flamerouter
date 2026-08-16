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

// GET/PATCH /api/cli-tools/{toolId}-settings.
func (s *Server) handleCLIToolSettings(w http.ResponseWriter, r *http.Request) {
	seg := r.PathValue("toolSettings")

	toolID, ok := strings.CutSuffix(seg, "-settings")
	if !ok || toolID == "" {
		writeErr(w, http.StatusBadRequest, "invalid tool settings path")
		return
	}

	m := s.cliTools()
	switch r.Method {
	case http.MethodGet:
		settings, err := m.GetSettings(toolID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		writeJSONOK(w, settings)
	case http.MethodPatch:
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}

		if err := m.PatchSettings(toolID, patch); err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		settings, err := m.GetSettings(toolID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		writeJSONOK(w, settings)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GET /api/cli-tools/all-statuses.
func (s *Server) handleAllCLIStatuses(w http.ResponseWriter, r *http.Request) {
	writeJSONOK(w, s.cliTools().AllStatuses())
}
