package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"flamerouter/internal/usage"
)

func (s *Server) handleUsageStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}
	if s.usageHub == nil {
		writeErr(w, http.StatusServiceUnavailable, "stream unavailable")
		return
	}
	s.usageHub.ServeHTTP(w, r)
}

func (s *Server) handleUsageStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}
	from, to := usageRange(r)
	rows, err := s.st.QueryUsageDaily(from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	req, prompt, completion := usage.SumDaily(rows)
	writeJSONOK(w, map[string]any{
		"from": from, "to": to,
		"requests": req, "promptTokens": prompt, "completionTokens": completion,
		"rows": len(rows),
	})
}

func (s *Server) handleUsageLogs(w http.ResponseWriter, r *http.Request) {
	s.handleRequestDetails(w, r)
}

func (s *Server) handleRequestLogs(w http.ResponseWriter, r *http.Request) {
	s.handleRequestDetails(w, r)
}

func (s *Server) handleRequestDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	rows, err := s.st.QueryRequestDetails(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		out = append(out, map[string]any{
			"id": d.ID, "timestamp": d.Timestamp, "provider": d.Provider, "model": d.Model,
			"connectionId": d.ConnectionID, "statusCode": d.StatusCode, "durationMs": d.DurationMs,
			"promptTokens": d.PromptTokens, "completionTokens": d.CompletionTokens,
			"errorText": d.ErrorText, "client": d.Client,
			"sourceFormat": d.SourceFormat, "targetFormat": d.TargetFormat,
		})
	}
	writeJSONOK(w, map[string]any{"data": out})
}

func (s *Server) handleUsageProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}
	from, to := usageRange(r)
	rows, err := s.st.QueryUsageDaily(from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	byProv := map[string]map[string]int{}
	for _, row := range rows {
		m := byProv[row.Provider]
		if m == nil {
			m = map[string]int{}
			byProv[row.Provider] = m
		}
		m["requests"] += row.Requests
		m["promptTokens"] += row.PromptTokens
		m["completionTokens"] += row.CompletionTokens
	}
	writeJSONOK(w, map[string]any{"from": from, "to": to, "providers": byProv})
}

func (s *Server) handleUsageHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}
	from, to := usageRange(r)
	rows, err := s.st.QueryUsageDaily(from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"from": from, "to": to, "data": rows})
}

func (s *Server) handleUsageChart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}
	from, to := usageRange(r)
	rows, err := s.st.QueryUsageChart(from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"from": from, "to": to, "data": rows})
}

func (s *Server) handleUsageByConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}
	connID := r.PathValue("connectionId")
	if connID == "" {
		// fallback: last path segment
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 {
			connID = parts[len(parts)-1]
		}
	}
	limit := 100
	rows, err := s.st.QueryRequestDetails(limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	out := make([]map[string]any, 0)
	var prompt, completion int
	for _, d := range rows {
		if d.ConnectionID != connID {
			continue
		}
		prompt += d.PromptTokens
		completion += d.CompletionTokens
		out = append(out, map[string]any{
			"id": d.ID, "timestamp": d.Timestamp, "provider": d.Provider, "model": d.Model,
			"statusCode": d.StatusCode, "durationMs": d.DurationMs,
			"promptTokens": d.PromptTokens, "completionTokens": d.CompletionTokens,
		})
	}

	conn, _ := s.st.GetConnection(connID)
	if conn == nil {
		writeJSONOK(w, map[string]any{
			"connectionId": connID,
			"promptTokens": prompt, "completionTokens": completion,
			"data":  out,
			"quota": usage.FetchQuota(""),
		})
		return
	}

	force := r.URL.Query().Get("force") == "1"
	usageRes := usage.FetchProviderUsage(r.Context(), usage.FetchOptions{
		Provider:             conn.Provider,
		AccessToken:          conn.AccessToken,
		APIKey:               conn.APIKey,
		BaseURL:              conn.BaseURL,
		ProviderSpecificData: conn.ProviderSpecificData,
		Force:                force,
	})

	writeJSONOK(w, usageRes)
}

func usageRange(r *http.Request) (from, to string) {
	from = r.URL.Query().Get("from")
	to = r.URL.Query().Get("to")
	if from == "" || to == "" {
		df, dt := usage.DefaultRange()
		if from == "" {
			from = df
		}
		if to == "" {
			to = dt
		}
	}
	// validate rough format
	if _, err := time.Parse("2006-01-02", from); err != nil {
		from, _ = usage.DefaultRange()
	}
	if _, err := time.Parse("2006-01-02", to); err != nil {
		_, to = usage.DefaultRange()
	}
	return
}
