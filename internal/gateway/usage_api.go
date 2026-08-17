package gateway

import (
	"context"
	"flamerouter/internal/store"
	"flamerouter/internal/usage"
	"net/http"
	"strconv"
	"strings"
	"time"
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

type usageItem struct {
	RawModel         string  `json:"rawModel,omitempty"`
	Provider         string  `json:"provider,omitempty"`
	ConnectionID     string  `json:"connectionId,omitempty"`
	AccountName      string  `json:"accountName,omitempty"`
	LastUsed         string  `json:"lastUsed,omitempty"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"promptTokens"`
	CompletionTokens int     `json:"completionTokens"`
	CachedTokens     int     `json:"cachedTokens"`
	Cost             float64 `json:"cost"`
}

func aggregateDailyUsage(dailyRows []store.UsageDaily) (int, int, int, map[string]map[string]any, map[string]*usageItem) {
	byProvider := make(map[string]map[string]any)
	byModel := make(map[string]*usageItem)

	totalRequests := 0
	totalPrompt := 0
	totalCompletion := 0

	for _, row := range dailyRows {
		totalRequests += row.Requests
		totalPrompt += row.PromptTokens
		totalCompletion += row.CompletionTokens

		pData, ok := byProvider[row.Provider]
		if !ok {
			pData = map[string]any{
				"requests": 0, "promptTokens": 0, "completionTokens": 0,
				"cachedTokens": 0, "cost": 0.0,
			}
			byProvider[row.Provider] = pData
		}

		pReq, _ := pData["requests"].(int)          //nolint:errcheck // safe type assertion
		pPrompt, _ := pData["promptTokens"].(int)   //nolint:errcheck // safe type assertion
		pComp, _ := pData["completionTokens"].(int) //nolint:errcheck // safe type assertion
		pData["requests"] = pReq + row.Requests
		pData["promptTokens"] = pPrompt + row.PromptTokens
		pData["completionTokens"] = pComp + row.CompletionTokens

		modelKey := row.Model
		if row.Provider != "" {
			modelKey = row.Model + " (" + row.Provider + ")"
		}

		mItem, ok := byModel[modelKey]
		if !ok {
			mItem = &usageItem{
				RawModel:         row.Model,
				Provider:         row.Provider,
				ConnectionID:     "",
				AccountName:      "",
				LastUsed:         row.Date,
				Requests:         0,
				PromptTokens:     0,
				CompletionTokens: 0,
				CachedTokens:     0,
				Cost:             0,
			}
			byModel[modelKey] = mItem
		}

		mItem.Requests += row.Requests
		mItem.PromptTokens += row.PromptTokens
		mItem.CompletionTokens += row.CompletionTokens

		if row.Date > mItem.LastUsed {
			mItem.LastUsed = row.Date
		}
	}

	return totalRequests, totalPrompt, totalCompletion, byProvider, byModel
}

func resolveAccountName(connID string, connMap map[string]string) string {
	if name, ok := connMap[connID]; ok && name != "" {
		return name
	}

	if len(connID) > 8 {
		return "Account " + connID[:8] + "..."
	}

	return "Account " + connID
}

func recordAccountUsage(byAccount map[string]*usageItem, d store.RequestDetail, accName string) {
	accKey := d.Model + " (" + d.Provider + " - " + accName + ")"

	aItem, ok := byAccount[accKey]
	if !ok {
		aItem = &usageItem{
			RawModel:         d.Model,
			Provider:         d.Provider,
			ConnectionID:     d.ConnectionID,
			AccountName:      accName,
			LastUsed:         d.Timestamp,
			Requests:         0,
			PromptTokens:     0,
			CompletionTokens: 0,
			CachedTokens:     0,
			Cost:             0,
		}
		byAccount[accKey] = aItem
	}

	aItem.Requests++
	aItem.PromptTokens += d.PromptTokens
	aItem.CompletionTokens += d.CompletionTokens

	if d.Timestamp > aItem.LastUsed {
		aItem.LastUsed = d.Timestamp
	}
}

func enrichUsageDetails(reqDetails []store.RequestDetail, connMap map[string]string, byModel map[string]*usageItem) map[string]*usageItem {
	byAccount := make(map[string]*usageItem)

	for _, d := range reqDetails {
		if d.ConnectionID != "" {
			accName := resolveAccountName(d.ConnectionID, connMap)
			recordAccountUsage(byAccount, d, accName)
		}

		modelKey := d.Model
		if d.Provider != "" {
			modelKey = d.Model + " (" + d.Provider + ")"
		}

		if mItem, ok := byModel[modelKey]; ok {
			if d.Timestamp > mItem.LastUsed {
				mItem.LastUsed = d.Timestamp
			}
		}
	}

	return byAccount
}

func (s *Server) buildRecentRequests(reqDetails []store.RequestDetail) []usage.RecentRequestItem {
	recent := make([]usage.RecentRequestItem, 0, 20)
	if s.usageHub != nil {
		recent = s.usageHub.GetRecent()
	}

	if len(recent) == 0 {
		for _, d := range reqDetails {
			status := "ok"
			if d.StatusCode >= 400 || d.ErrorText != "" {
				status = "error"
			}

			recent = append(recent, usage.RecentRequestItem{
				Timestamp:        d.Timestamp,
				Model:            d.Model,
				Provider:         d.Provider,
				PromptTokens:     d.PromptTokens,
				CompletionTokens: d.CompletionTokens,
				DurationMs:       int64(d.DurationMs),
				Status:           status,
			})
			if len(recent) >= 20 {
				break
			}
		}
	}

	return recent
}

func (s *Server) handleUsageStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}

	period := r.URL.Query().Get("period")
	if period == "" {
		period = "today"
	}

	from, to := usageRange(r)

	dailyRows, err := s.st.QueryUsageDaily(from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	connMap := make(map[string]string)

	if conns, errList := s.st.ListAllConnections(); errList == nil {
		for _, c := range conns {
			name := c.Name
			if name == "" {
				name = c.ID
			}

			connMap[c.ID] = name
		}
	}

	reqDetails, errDetails := s.st.QueryRequestDetails(100)
	if errDetails != nil {
		reqDetails = nil
	}

	totalReqs, totalPrompt, totalCompletion, byProvider, byModel := aggregateDailyUsage(dailyRows)
	byAccount := enrichUsageDetails(reqDetails, connMap, byModel)
	recent := s.buildRecentRequests(reqDetails)

	writeJSONOK(w, map[string]any{
		"period":                period,
		"from":                  from,
		"to":                    to,
		"totalRequests":         totalReqs,
		"totalPromptTokens":     totalPrompt,
		"totalCompletionTokens": totalCompletion,
		"totalCachedTokens":     0,
		"totalCost":             0.0,
		"byProvider":            byProvider,
		"byModel":               byModel,
		"byAccount":             byAccount,
		"byApiKey":              map[string]any{},
		"byEndpoint":            map[string]any{},
		"recentRequests":        recent,
		"activeRequests":        []any{},
		"activeConnections":     len(connMap),
	})
}

func (s *Server) handleUsageLogs(w http.ResponseWriter, r *http.Request) {
	s.handleRequestDetails(w, r)
}

func (s *Server) handleRequestLogs(w http.ResponseWriter, r *http.Request) {
	s.handleRequestDetails(w, r)
}

func filterRequestDetails(rows []store.RequestDetail, providerFilter, startDate, endDate string) []store.RequestDetail {
	filtered := make([]store.RequestDetail, 0, len(rows))

	for _, d := range rows {
		if providerFilter != "" && !strings.EqualFold(d.Provider, providerFilter) {
			continue
		}

		if startDate != "" && d.Timestamp < startDate {
			continue
		}

		if endDate != "" && d.Timestamp > endDate {
			continue
		}

		filtered = append(filtered, d)
	}

	return filtered
}

func paginateSlice[T any](items []T, page, pageSize int) ([]T, int, int) {
	totalItems := len(items)
	totalPages := 1

	if pageSize > 0 {
		totalPages = (totalItems + pageSize - 1) / pageSize
	}

	if totalPages == 0 {
		totalPages = 1
	}

	startIdx := (page - 1) * pageSize
	endIdx := startIdx + pageSize

	if startIdx > totalItems {
		startIdx = totalItems
	}

	if endIdx > totalItems {
		endIdx = totalItems
	}

	return items[startIdx:endIdx], totalItems, totalPages
}

func formatRequestDetailItem(d store.RequestDetail) map[string]any {
	status := "ok"
	if d.StatusCode >= 400 || d.ErrorText != "" {
		status = "error"
	}

	return map[string]any{
		"id":           d.ID,
		"timestamp":    d.Timestamp,
		"provider":     d.Provider,
		"model":        d.Model,
		"connectionId": d.ConnectionID,
		"status":       status,
		"statusCode":   d.StatusCode,
		"latency": map[string]any{
			"total": d.DurationMs,
		},
		"tokens": map[string]any{
			"prompt_tokens":     d.PromptTokens,
			"completion_tokens": d.CompletionTokens,
		},
		"errorText":    d.ErrorText,
		"client":       d.Client,
		"sourceFormat": d.SourceFormat,
		"targetFormat": d.TargetFormat,
		"request": map[string]any{
			"redacted": true,
		},
		"response": map[string]any{
			"redacted": true,
		},
	}
}

func parsePaginationParams(r *http.Request) (int, int) {
	limit := 100

	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	page := 1

	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}

	pageSize := limit

	if v := r.URL.Query().Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			pageSize = n
		}
	}

	return page, pageSize
}

func (s *Server) handleRequestDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}

	page, pageSize := parsePaginationParams(r)
	providerFilter := r.URL.Query().Get("provider")
	startDate := r.URL.Query().Get("startDate")
	endDate := r.URL.Query().Get("endDate")

	rows, err := s.st.QueryRequestDetails(500)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	filtered := filterRequestDetails(rows, providerFilter, startDate, endDate)
	pageRows, totalItems, totalPages := paginateSlice(filtered, page, pageSize)

	out := make([]map[string]any, 0, len(pageRows))
	for _, d := range pageRows {
		out = append(out, formatRequestDetailItem(d))
	}

	writeJSONOK(w, map[string]any{
		"data":    out,
		"details": out,
		"pagination": map[string]any{
			"page":       page,
			"pageSize":   pageSize,
			"totalItems": totalItems,
			"totalPages": totalPages,
			"hasNext":    page < totalPages,
			"hasPrev":    page > 1,
		},
	})
}

func (s *Server) handleUsageProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}
	// Return list of distinct provider objects {id, name} for RequestDetailsTab
	reqDetails, err := s.st.QueryRequestDetails(500)
	if err != nil {
		reqDetails = nil
	}

	seen := make(map[string]bool)
	providers := make([]map[string]string, 0)

	for _, d := range reqDetails {
		if d.Provider != "" && !seen[d.Provider] {
			seen[d.Provider] = true

			providers = append(providers, map[string]string{
				"id":   d.Provider,
				"name": d.Provider,
			})
		}
	}

	writeJSONOK(w, map[string]any{"providers": providers})
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

	type chartPoint struct {
		Label  string  `json:"label"`
		Tokens int     `json:"tokens"`
		Cost   float64 `json:"cost"`
	}

	points := make([]chartPoint, 0, len(rows))

	for _, row := range rows {
		lbl := row.Date
		if t, err := time.Parse("2006-01-02", row.Date); err == nil {
			lbl = t.Format("Jan 2")
		}

		points = append(points, chartPoint{
			Label:  lbl,
			Tokens: row.PromptTokens + row.CompletionTokens,
			Cost:   0.0,
		})
	}

	writeJSONOK(w, points)
}

func (s *Server) extractConnUsageData(connID string, limit int) ([]map[string]any, int, int, error) {
	rows, err := s.st.QueryRequestDetails(limit)
	if err != nil {
		return nil, 0, 0, err
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

	return out, prompt, completion, nil
}

func isAuthExpiredQuota(res *usage.QuotaResult) bool {
	if res == nil || res.Message == "" {
		return false
	}

	msg := strings.ToLower(res.Message)
	patterns := []string{"expired", "authentication", "unauthorized", "401", "re-authorize", "re-auth"}

	for _, p := range patterns {
		if strings.Contains(msg, p) {
			return true
		}
	}

	return false
}

func (s *Server) callQuotaFetch(ctx context.Context, conn *store.Connection, force bool) *usage.QuotaResult {
	return usage.FetchProviderUsage(ctx, usage.FetchOptions{
		ProviderSpecificData: conn.ProviderSpecificData,
		HTTPClient:           nil,
		Provider:             conn.Provider,
		AccessToken:          conn.AccessToken,
		APIKey:               conn.APIKey,
		BaseURL:              conn.BaseURL,
		Force:                force,
	})
}

func (s *Server) refreshConnection(ctx context.Context, conn *store.Connection, force bool) *store.Connection {
	if s.credMgr == nil || conn.RefreshToken == "" {
		return conn
	}

	var (
		refreshed *store.Connection
		err       error
	)

	if force {
		refreshed, err = s.credMgr.RefreshForce(ctx, s.st, conn)
	} else {
		refreshed, err = s.credMgr.RefreshIfNeeded(ctx, s.st, conn)
	}

	if err == nil && refreshed != nil {
		return refreshed
	}

	return conn
}

func (s *Server) fetchConnQuota(ctx context.Context, conn *store.Connection, force bool) *usage.QuotaResult {
	conn = s.refreshConnection(ctx, conn, false)
	usageRes := s.callQuotaFetch(ctx, conn, force)

	if isAuthExpiredQuota(usageRes) && conn.RefreshToken != "" {
		refreshed := s.refreshConnection(ctx, conn, true)
		if refreshed != conn {
			usageRes = s.callQuotaFetch(ctx, refreshed, force)
		}
	}

	return usageRes
}

func (s *Server) handleUsageByConnection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method")
		return
	}

	connID := r.PathValue("connectionId")
	if connID == "" {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) > 0 {
			connID = parts[len(parts)-1]
		}
	}

	out, prompt, completion, err := s.extractConnUsageData(connID, 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	conn, err := s.st.GetConnection(connID)
	if err != nil || conn == nil {
		writeJSONOK(w, map[string]any{
			"connectionId": connID,
			"promptTokens": prompt, "completionTokens": completion,
			"data":  out,
			"quota": usage.FetchQuota(""),
		})

		return
	}

	force := r.URL.Query().Get("force") == "1"
	usageRes := s.fetchConnQuota(r.Context(), conn, force)

	writeJSONOK(w, map[string]any{
		"connectionId": connID,
		"promptTokens": prompt, "completionTokens": completion,
		"data":  out,
		"quota": usageRes,
	})
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
