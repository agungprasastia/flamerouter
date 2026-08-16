package gateway

import (
	"flamerouter/internal/infra/pxpipe"
	"net/http"
)

var pxpipeProc = pxpipe.New()

func (s *Server) handlePxpipeInstall(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	if err := pxpipeProc.Install(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "status": pxpipeProc.Status()})
		return
	}

	writeJSONOK(w, map[string]any{"success": true, "status": pxpipeProc.Status()})
}

func (s *Server) handlePxpipeStart(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	url := pxpipeProc.URL()
	if err := pxpipeProc.Start(url); err != nil {
		status := http.StatusInternalServerError
		if pxpipeProc.Status() == "not_installed" {
			status = http.StatusBadRequest
		}

		writeJSON(w, status, map[string]any{"error": err.Error(), "status": pxpipeProc.Status()})

		return
	}

	writeJSONOK(w, map[string]any{"success": true, "status": pxpipeProc.Status(), "url": pxpipeProc.URL()})
}

func (s *Server) handlePxpipeStop(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	_ = pxpipeProc.Stop()
	writeJSONOK(w, map[string]any{"success": true, "status": pxpipeProc.Status()})
}

func (s *Server) handlePxpipeRestart(w http.ResponseWriter, r *http.Request) {
	if !requireLocal(w, r) {
		return
	}

	if err := pxpipeProc.Restart(pxpipeProc.URL()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "status": pxpipeProc.Status()})
		return
	}

	writeJSONOK(w, map[string]any{"success": true, "status": pxpipeProc.Status()})
}

func (s *Server) handlePxpipeStatus(w http.ResponseWriter, r *http.Request) {
	enabled := false

	var minChars any

	var timeoutMs any

	if s.st != nil {
		if v, _ := s.st.GetSetting("pxpipeEnabled"); v == "true" || v == "1" {
			enabled = true
		}

		if v, _ := s.st.GetSetting("pxpipeMinChars"); v != "" {
			minChars = v
		}

		if v, _ := s.st.GetSetting("pxpipeTimeoutMs"); v != "" {
			timeoutMs = v
		}
	}

	writeJSONOK(w, map[string]any{
		"status":    pxpipeProc.Status(),
		"url":       pxpipeProc.URL(),
		"healthy":   pxpipeProc.Health(),
		"enabled":   enabled,
		"minChars":  minChars,
		"timeoutMs": timeoutMs,
	})
}

func (s *Server) handlePxpipeHealth(w http.ResponseWriter, r *http.Request) {
	ok := pxpipeProc.Health()
	writeJSONOK(w, map[string]any{"healthy": ok, "status": pxpipeProc.Status()})
}

func (s *Server) handlePxpipeStats(w http.ResponseWriter, r *http.Request) {
	writeJSONOK(w, pxpipeProc.Stats())
}

func (s *Server) handlePxpipeLogs(w http.ResponseWriter, r *http.Request) {
	writeJSONOK(w, map[string]any{"logs": pxpipeProc.Logs()})
}
