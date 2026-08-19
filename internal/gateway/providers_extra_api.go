package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"flamerouter/internal/netutil"
	"flamerouter/internal/provider"
	"flamerouter/internal/store"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	openaiCompatiblePrefix    = "openai-compatible-"
	anthropicCompatiblePrefix = "anthropic-compatible-"
	kiloModelsURL             = "https://api.kilo.ai/api/gateway/models"
	kiloCacheTTL              = time.Hour
)

var (
	kiloMu      sync.Mutex
	kiloCache   []map[string]any
	kiloCacheAt time.Time
)

func maskLongTokenName(name string) string {
	if len(name) <= 16 {
		return name
	}

	if !isAlphanumericOrUnderscore(name) {
		return name
	}

	if len(name) >= 32 {
		return name[:8] + "***"
	}

	return name
}

func isAlphanumericOrUnderscore(s string) bool {
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
			return false
		}
	}

	return true
}

func extractSafePSD(psd map[string]any, baseURL string) map[string]any {
	safePSD := map[string]any{}

	for _, f := range []string{
		"baseUrl", "azureEndpoint", "deployment", "apiVersion", "accountId",
		"region", "projectId", "resourceUrl", "proxyPoolId",
		"connectionProxyEnabled", "connectionProxyUrl", "connectionNoProxy",
		"githubLogin", "githubName", "githubEmail", "githubUserId",
		"username", "firstName", "lastName", "authMethod", "authKind", "profileArn",
	} {
		if v, ok := psd[f]; ok {
			safePSD[f] = v
		}
	}

	if baseURL != "" {
		if _, has := safePSD["baseUrl"]; !has {
			safePSD["baseUrl"] = baseURL
		}
	}

	return safePSD
}

// Safe connection fields for browser (no secrets).
func sanitizeConnClient(c store.Connection) map[string]any {
	name := maskLongTokenName(c.Name)

	out := map[string]any{
		"id": c.ID, "provider": c.Provider, "authType": c.AuthType, "name": name,
		"priority": c.Priority, "isActive": c.IsActive,
		"testStatus": c.TestStatus, "lastError": c.LastError, "expiresAt": c.ExpiresAt,
		"lastUsedAt": c.LastUsedAt, "consecutiveUseCount": c.ConsecutiveUseCount,
	}

	safePSD := extractSafePSD(c.ProviderSpecificData, c.BaseURL)
	if len(safePSD) > 0 {
		out["providerSpecificData"] = safePSD
	}

	return out
}

func filterConns(all []store.Connection, providerFilter, accountStatus string) ([]store.Connection, []string) {
	providerOptsSet := map[string]struct{}{}
	filtered := make([]store.Connection, 0, len(all))

	for _, c := range all {
		providerOptsSet[c.Provider] = struct{}{}

		if providerFilter != "all" && c.Provider != providerFilter {
			continue
		}

		if accountStatus == "active" && !c.IsActive {
			continue
		}

		if accountStatus == "inactive" && c.IsActive {
			continue
		}

		filtered = append(filtered, c)
	}

	providerOptions := make([]string, 0, len(providerOptsSet))
	for p := range providerOptsSet {
		providerOptions = append(providerOptions, p)
	}

	sort.Strings(providerOptions)

	return filtered, providerOptions
}

func filterAndSortConns(all []store.Connection, providerFilter, accountStatus, sortBy string) ([]store.Connection, []string) {
	filtered, providerOptions := filterConns(all, providerFilter, accountStatus)

	if sortBy == "provider" {
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Provider < filtered[j].Provider
		})
	} else {
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].Priority != filtered[j].Priority {
				return filtered[i].Priority < filtered[j].Priority
			}

			return filtered[i].Provider < filtered[j].Provider
		})
	}

	return filtered, providerOptions
}

func computeTotalPages(total, pageSize int) int {
	totalPages := total / pageSize
	if total%pageSize != 0 || totalPages == 0 {
		if total == 0 {
			totalPages = 1
		} else if total%pageSize != 0 {
			totalPages++
		}
	}

	return totalPages
}

func slicePage(filtered []store.Connection, page, pageSize, total int) []map[string]any {
	offset := (page - 1) * pageSize
	end := offset + pageSize

	if offset > total {
		offset = total
	}

	if end > total {
		end = total
	}

	pageConns := make([]map[string]any, 0, end-offset)
	for _, c := range filtered[offset:end] {
		pageConns = append(pageConns, sanitizeConnClient(c))
	}

	return pageConns
}

// GET /api/providers/client — sanitized connections for dashboard.
func (s *Server) handleProvidersClient(w http.ResponseWriter, r *http.Request) {
	providerFilter := r.URL.Query().Get("provider")
	if providerFilter == "" {
		providerFilter = "all"
	}

	accountStatus := r.URL.Query().Get("accountStatus")
	if accountStatus == "" {
		accountStatus = "all"
	}

	sortBy := r.URL.Query().Get("sort")
	if sortBy == "" {
		sortBy = "priority"
	}

	page := parsePositiveInt(r.URL.Query().Get("page"), 1)

	pageSize := parsePositiveInt(r.URL.Query().Get("pageSize"), 20)
	if pageSize > 500 {
		pageSize = 500
	}

	all, err := s.st.ListAllConnections()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to fetch providers")
		return
	}

	filtered, providerOptions := filterAndSortConns(all, providerFilter, accountStatus, sortBy)

	total := len(filtered)
	totalPages := computeTotalPages(total, pageSize)

	if page > totalPages {
		page = totalPages
	}

	pageConns := slicePage(filtered, page, pageSize, total)

	reg := provider.ListProviders()
	writeJSONOK(w, map[string]any{
		"connections":     pageConns,
		"providerOptions": providerOptions,
		"pagination": map[string]any{
			"page": page, "pageSize": pageSize, "total": total, "totalPages": totalPages,
		},
		"totals": map[string]any{
			"eligibleConnections":         len(all),
			"providerFilteredConnections": len(filtered),
			"registryProviders":           len(reg),
		},
	})
}

func parsePositiveInt(s string, fallback int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return fallback
	}

	return n
}

func fetchModelsJSON(ctx context.Context, u string) any {
	client := &http.Client{
		Timeout:       15 * time.Second,
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}

	resp, err := netutil.DoHTTP(client, req)
	if err != nil || resp.StatusCode >= 400 {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close() //nolint:errcheck,gosec // best-effort body close
		}

		return nil
	}

	defer resp.Body.Close() //nolint:errcheck // best-effort body close

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var jsonAny any
	if err := json.Unmarshal(body, &jsonAny); err != nil {
		return nil
	}

	return jsonAny
}

// GET /api/providers/suggested-models?url=&type=.
func (s *Server) handleSuggestedModels(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	typ := r.URL.Query().Get("type")

	if u == "" || typ == "" {
		writeErr(w, http.StatusBadRequest, "Missing url or type")
		return
	}

	if err := netutil.AssertPublicURL(u); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid or disallowed url: "+err.Error())
		return
	}

	filter := suggestedFilters[typ]
	if filter == nil {
		writeErr(w, http.StatusBadRequest, "Unknown filter type")
		return
	}

	jsonAny := fetchModelsJSON(r.Context(), u)
	if jsonAny == nil {
		writeJSONOK(w, map[string]any{"data": []any{}})
		return
	}

	raw := extractModelsArray(jsonAny)
	writeJSONOK(w, map[string]any{"data": filter(raw)})
}

func extractModelsArray(v any) []map[string]any {
	switch t := v.(type) {
	case map[string]any:
		if d, ok := t["data"].([]any); ok {
			return asMapSlice(d)
		}

		if d, ok := t["models"].([]any); ok {
			return asMapSlice(d)
		}
	case []any:
		return asMapSlice(t)
	}

	return nil
}

func asMapSlice(arr []any) []map[string]any {
	out := make([]map[string]any, 0, len(arr))

	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}

	return out
}

var suggestedFilters = map[string]func([]map[string]any) []map[string]any{
	"openrouter-free": func(models []map[string]any) []map[string]any {
		var out []map[string]any
		for _, m := range models {
			pricing, _ := m["pricing"].(map[string]any) //nolint:errcheck // safe type assertion
			prompt, _ := pricing["prompt"].(string)     //nolint:errcheck // safe type assertion
			comp, _ := pricing["completion"].(string)   //nolint:errcheck // safe type assertion
			ctxLen := asFloat(m["context_length"])
			if prompt == "0" && comp == "0" && ctxLen >= 200000 {
				out = append(out, map[string]any{
					"id": m["id"], "name": m["name"], "contextLength": ctxLen,
				})
			}
		}
		sort.Slice(out, func(i, j int) bool {
			a := asFloat(out[i]["contextLength"])
			b := asFloat(out[j]["contextLength"])
			return a > b
		})
		return out
	},
	"opencode-free": func(models []map[string]any) []map[string]any {
		known := map[string]bool{"big-pickle": true}
		var out []map[string]any
		for _, m := range models {
			id, _ := m["id"].(string) //nolint:errcheck // safe type assertion
			if strings.HasSuffix(id, "-free") || known[id] {
				out = append(out, map[string]any{"id": id, "name": id})
			}
		}
		return out
	},
	"mimo-free": func(models []map[string]any) []map[string]any {
		var out []map[string]any
		for _, m := range models {
			id, _ := m["id"].(string)     //nolint:errcheck // safe type assertion
			name, _ := m["name"].(string) //nolint:errcheck // safe type assertion
			if strings.HasPrefix(id, "mimo") || strings.Contains(strings.ToLower(name), "mimo") {
				if name == "" {
					name = id
				}
				out = append(out, map[string]any{"id": id, "name": name})
			}
		}
		return out
	},
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f
		}

		return 0
	default:
		return 0
	}
}

func matchTestBatchMode(c store.Connection, mode, providerID string) (bool, bool) {
	switch mode {
	case "all":
		return true, true
	case "provider":
		return providerID != "" && c.Provider == providerID, true
	case "oauth":
		return c.AuthType == "oauth", true
	case "apikey":
		return c.AuthType == "api_key" || c.AuthType == "apikey", true
	default:
		return matchTestBatchModeExtra(c, mode)
	}
}

func matchTestBatchModeExtra(c store.Connection, mode string) (bool, bool) {
	switch mode {
	case "free":
		p := provider.GetProvider(c.Provider)
		return p != nil && p.HasFree, true
	case "compatible":
		return strings.HasPrefix(c.Provider, openaiCompatiblePrefix) || strings.HasPrefix(c.Provider, anthropicCompatiblePrefix), true
	default:
		return false, false
	}
}

func (s *Server) selectConnsForBatchTest(ids []string, mode, providerID string) ([]store.Connection, bool, error) {
	if len(ids) > 0 {
		return s.selectConnsByID(ids), true, nil
	}

	if mode == "" {
		return nil, false, nil
	}

	return s.selectConnsByMode(mode, providerID)
}

func (s *Server) selectConnsByID(ids []string) []store.Connection {
	toTest := make([]store.Connection, 0, len(ids))

	for _, id := range ids {
		c, err := s.st.GetConnection(id)
		if err != nil || c == nil {
			continue
		}

		toTest = append(toTest, *c)
	}

	return toTest
}

func (s *Server) selectConnsByMode(mode, providerID string) ([]store.Connection, bool, error) {
	all, err := s.st.ListAllConnections()
	if err != nil {
		return nil, true, err
	}

	toTest := make([]store.Connection, 0, len(all))

	for _, c := range all {
		if !c.IsActive {
			continue
		}

		match, validMode := matchTestBatchMode(c, mode, providerID)
		if !validMode {
			return nil, false, nil
		}

		if match {
			toTest = append(toTest, c)
		}
	}

	return toTest, true, nil
}

type batchTestReq struct {
	Mode       string   `json:"mode"`
	ProviderID string   `json:"providerId"`
	IDs        []string `json:"ids"`
}

func formatBatchTestResults(toTest []store.Connection) ([]map[string]any, int) {
	results := make([]map[string]any, 0, len(toTest))
	passed := 0

	for _, conn := range toTest {
		valid := conn.APIKey != "" || conn.AccessToken != ""
		if valid {
			passed++
		}

		var errMsg any
		if !valid {
			errMsg = "no credentials"
		}

		name := conn.Name
		if name == "" {
			name = conn.Provider
		}

		results = append(results, map[string]any{
			"provider":       conn.Provider,
			"connectionId":   conn.ID,
			"connectionName": name,
			"authType":       conn.AuthType,
			"valid":          valid,
			"latencyMs":      0,
			"error":          errMsg,
			"testedAt":       time.Now().UTC().Format(time.RFC3339),
		})
	}

	return results, passed
}

// POST /api/providers/test-batch — body {ids:[]} or {mode, providerId}.
func (s *Server) handleProviderTestBatch(w http.ResponseWriter, r *http.Request) {
	var body batchTestReq
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	toTest, ok, err := s.selectConnsForBatchTest(body.IDs, body.Mode, body.ProviderID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Batch test failed")
		return
	}

	if !ok {
		if body.Mode == "" && len(body.IDs) == 0 {
			writeErr(w, http.StatusBadRequest, "mode is required")
		} else {
			writeErr(w, http.StatusBadRequest, "Invalid mode. Use: provider, oauth, free, apikey, compatible, all")
		}

		return
	}

	results, passed := formatBatchTestResults(toTest)

	writeJSONOK(w, map[string]any{
		"mode":       body.Mode,
		"providerId": nullIfEmpty(body.ProviderID),
		"results":    results,
		"testedAt":   time.Now().UTC().Format(time.RFC3339),
		"summary": map[string]any{
			"total": len(results), "passed": passed, "failed": len(results) - passed,
		},
	})
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}

	return s
}

func requestKiloModels(ctx context.Context) ([]map[string]any, error) {
	client := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, kiloModelsURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := netutil.DoHTTP(client, req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close() //nolint:errcheck // best-effort body close

	if resp.StatusCode >= 400 {
		return nil, sql.ErrConnDone
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Data []map[string]any `json:"data"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	free := make([]map[string]any, 0)

	for _, m := range payload.Data {
		if isFree, _ := m["isFree"].(bool); isFree { //nolint:errcheck // safe type assertion
			ctxLen := asFloat(m["context_length"])
			free = append(free, map[string]any{
				"id": m["id"], "name": m["name"], "isFree": true, "context_length": ctxLen,
			})
		}
	}

	return free, nil
}

// GET /api/providers/kilo/free-models.
func (s *Server) handleKiloFreeModels(w http.ResponseWriter, r *http.Request) {
	kiloMu.Lock()
	defer kiloMu.Unlock()

	now := time.Now()
	if kiloCache != nil && now.Sub(kiloCacheAt) < kiloCacheTTL {
		writeJSONOK(w, map[string]any{"models": kiloCache, "cached": true})
		return
	}

	free, err := requestKiloModels(r.Context())
	if err != nil {
		if kiloCache != nil {
			writeJSONOK(w, map[string]any{"models": kiloCache, "cached": true, "warning": err.Error()})
			return
		}

		writeJSON(w, http.StatusBadGateway, map[string]any{
			"models": []any{}, "error": "Failed to fetch Kilo models: " + err.Error(),
		})

		return
	}

	kiloCache = free
	kiloCacheAt = now

	writeJSONOK(w, map[string]any{"models": free, "cached": false})
}

func buildRegistryNotice(notice *provider.Notice) map[string]any {
	if notice == nil {
		return nil
	}

	n := map[string]any{}
	if notice.Text != "" {
		n["text"] = notice.Text
	}

	if notice.SignupURL != "" {
		n["signupUrl"] = notice.SignupURL
	}

	if notice.APIKeyURL != "" {
		n["apiKeyUrl"] = notice.APIKeyURL
	}

	if len(n) == 0 {
		return nil
	}

	return n
}

// GET /api/providers/registry — expose full provider registry.
func (s *Server) handleProviderRegistry(w http.ResponseWriter, _ *http.Request) {
	all := provider.ListProviders()
	out := make([]map[string]any, 0, len(all))

	for _, p := range all {
		models := make([]map[string]any, 0, len(p.Models))
		for _, m := range p.Models {
			models = append(models, map[string]any{
				"id":   m.ID,
				"name": m.Name,
				"kind": m.Kind,
			})
		}

		entry := map[string]any{
			"id":       p.ID,
			"name":     p.Display.Name,
			"category": p.Category,
			"hasFree":  p.HasFree,
			"models":   models,
			"color":    p.Display.Color,
			"textIcon": p.Display.TextIcon,
			"website":  p.Display.Website,
		}

		if p.Display.Deprecated {
			entry["deprecated"] = true
			entry["deprecationNotice"] = p.Display.DeprecNotice
		}

		if n := buildRegistryNotice(p.Display.Notice); n != nil {
			entry["notice"] = n
		}

		out = append(out, entry)
	}

	writeJSONOK(w, map[string]any{"registry": out})
}

// PUT /api/models/alias — set model alias (9router dashboard body: {model, alias}).
func (s *Server) handleUpdateAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
		Alias string `json:"alias"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" || req.Alias == "" {
		writeErr(w, http.StatusBadRequest, "model and alias required")
		return
	}

	if err := s.st.SetAlias(req.Alias, req.Model); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, map[string]any{"success": true, "model": req.Model, "alias": req.Alias})
}

// DELETE /api/models/alias — delete model alias (?alias= or {alias}).
func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	alias := r.URL.Query().Get("alias")
	if alias == "" {
		var req struct {
			Alias string `json:"alias"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Alias == "" {
			writeErr(w, http.StatusBadRequest, "alias required")
			return
		}

		alias = req.Alias
	}

	if err := s.st.DeleteAlias(alias); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, map[string]any{"ok": true})
}

// DELETE /api/models/custom — delete custom model (?providerAlias=&id= or {id}).
func (s *Server) handleDeleteCustomModel(w http.ResponseWriter, r *http.Request) {
	providerAlias := r.URL.Query().Get("providerAlias")
	modelID := r.URL.Query().Get("id")

	if providerAlias != "" && modelID != "" {
		if err := s.st.DeleteCustomModelByModel(providerAlias, modelID); err != nil {
			if err == sql.ErrNoRows {
				writeErr(w, http.StatusNotFound, "custom model not found")
				return
			}

			writeErr(w, http.StatusInternalServerError, "db")

			return
		}

		writeJSONOK(w, map[string]any{"ok": true})

		return
	}

	var req struct {
		ID string `json:"id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeErr(w, http.StatusBadRequest, "id required")
		return
	}

	if err := s.st.DeleteCustomModel(req.ID); err != nil {
		if err == sql.ErrNoRows {
			writeErr(w, http.StatusNotFound, "custom model not found")
			return
		}

		writeErr(w, http.StatusInternalServerError, "db")

		return
	}

	writeJSONOK(w, map[string]any{"ok": true})
}
