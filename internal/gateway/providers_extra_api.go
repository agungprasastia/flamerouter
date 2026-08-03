package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"flamerouter/internal/provider"
	"flamerouter/internal/store"
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

// Safe connection fields for browser (no secrets).
func sanitizeConnClient(c store.Connection) map[string]any {
	name := c.Name
	if len(name) > 16 {
		// mask long token-like names
		ok := false
		for _, ch := range name {
			if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
				ok = true
			} else {
				ok = false
				break
			}
		}
		if ok && len(name) >= 32 {
			name = name[:8] + "***"
		}
	}
	out := map[string]any{
		"id": c.ID, "provider": c.Provider, "authType": c.AuthType, "name": name,
		"priority": c.Priority, "isActive": c.IsActive,
		"testStatus": c.TestStatus, "lastError": c.LastError, "expiresAt": c.ExpiresAt,
		"lastUsedAt": c.LastUsedAt, "consecutiveUseCount": c.ConsecutiveUseCount,
	}
	if c.ProviderSpecificData != nil {
		safePSD := map[string]any{}
		for _, f := range []string{
			"baseUrl", "azureEndpoint", "deployment", "apiVersion", "accountId",
			"region", "projectId", "resourceUrl", "proxyPoolId",
			"connectionProxyEnabled", "connectionProxyUrl", "connectionNoProxy",
			"githubLogin", "githubName", "githubEmail", "githubUserId",
			"username", "firstName", "lastName", "authMethod", "authKind", "profileArn",
		} {
			if v, ok := c.ProviderSpecificData[f]; ok {
				safePSD[f] = v
			}
		}
		if len(safePSD) > 0 {
			out["providerSpecificData"] = safePSD
		}
	}
	if c.BaseURL != "" {
		if psd, ok := out["providerSpecificData"].(map[string]any); ok {
			if _, has := psd["baseUrl"]; !has {
				psd["baseUrl"] = c.BaseURL
			}
		} else {
			out["providerSpecificData"] = map[string]any{"baseUrl": c.BaseURL}
		}
	}
	return out
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

	// eligible: all connections (usage filter soft — registry has no features.usage yet)
	eligible := all
	providerOptsSet := map[string]struct{}{}
	for _, c := range eligible {
		providerOptsSet[c.Provider] = struct{}{}
	}
	var providerOptions []string
	for p := range providerOptsSet {
		providerOptions = append(providerOptions, p)
	}
	sort.Strings(providerOptions)

	filtered := make([]store.Connection, 0, len(eligible))
	for _, c := range eligible {
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

	total := len(filtered)
	totalPages := total / pageSize
	if total%pageSize != 0 || totalPages == 0 {
		if total == 0 {
			totalPages = 1
		} else if total%pageSize != 0 {
			totalPages++
		}
	}
	if page > totalPages {
		page = totalPages
	}
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

	// also include registry summary counts (brief step 1)
	reg := provider.ListProviders()
	writeJSONOK(w, map[string]any{
		"connections":     pageConns,
		"providerOptions": providerOptions,
		"pagination": map[string]any{
			"page": page, "pageSize": pageSize, "total": total, "totalPages": totalPages,
		},
		"totals": map[string]any{
			"eligibleConnections":         len(eligible),
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

// GET /api/providers/suggested-models?url=&type=
func (s *Server) handleSuggestedModels(w http.ResponseWriter, r *http.Request) {
	u := r.URL.Query().Get("url")
	typ := r.URL.Query().Get("type")
	if u == "" || typ == "" {
		writeErr(w, http.StatusBadRequest, "Missing url or type")
		return
	}
	filter := suggestedFilters[typ]
	if filter == nil {
		writeErr(w, http.StatusBadRequest, "Unknown filter type")
		return
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u)
	if err != nil || resp.StatusCode >= 400 {
		if resp != nil {
			resp.Body.Close()
		}
		writeJSONOK(w, map[string]any{"data": []any{}})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var jsonAny any
	if err := json.Unmarshal(body, &jsonAny); err != nil {
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
			pricing, _ := m["pricing"].(map[string]any)
			prompt, _ := pricing["prompt"].(string)
			comp, _ := pricing["completion"].(string)
			ctxLen, _ := asFloat(m["context_length"])
			if prompt == "0" && comp == "0" && ctxLen >= 200000 {
				out = append(out, map[string]any{
					"id": m["id"], "name": m["name"], "contextLength": ctxLen,
				})
			}
		}
		sort.Slice(out, func(i, j int) bool {
			a, _ := asFloat(out[i]["contextLength"])
			b, _ := asFloat(out[j]["contextLength"])
			return a > b
		})
		return out
	},
	"opencode-free": func(models []map[string]any) []map[string]any {
		known := map[string]bool{"big-pickle": true}
		var out []map[string]any
		for _, m := range models {
			id, _ := m["id"].(string)
			if strings.HasSuffix(id, "-free") || known[id] {
				out = append(out, map[string]any{"id": id, "name": id})
			}
		}
		return out
	},
	"mimo-free": func(models []map[string]any) []map[string]any {
		var out []map[string]any
		for _, m := range models {
			id, _ := m["id"].(string)
			name, _ := m["name"].(string)
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

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// POST /api/providers/test-batch — body {ids:[]} or {mode, providerId}
func (s *Server) handleProviderTestBatch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs        []string `json:"ids"`
		Mode       string   `json:"mode"`
		ProviderID string   `json:"providerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	var toTest []store.Connection
	if len(body.IDs) > 0 {
		for _, id := range body.IDs {
			c, err := s.st.GetConnection(id)
			if err != nil || c == nil {
				continue
			}
			toTest = append(toTest, *c)
		}
	} else {
		if body.Mode == "" {
			writeErr(w, http.StatusBadRequest, "mode is required")
			return
		}
		all, err := s.st.ListAllConnections()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Batch test failed")
			return
		}
		for _, c := range all {
			if !c.IsActive {
				continue
			}
			switch body.Mode {
			case "all":
				toTest = append(toTest, c)
			case "provider":
				if body.ProviderID != "" && c.Provider == body.ProviderID {
					toTest = append(toTest, c)
				}
			case "oauth":
				if c.AuthType == "oauth" {
					toTest = append(toTest, c)
				}
			case "apikey":
				if c.AuthType == "api_key" || c.AuthType == "apikey" {
					toTest = append(toTest, c)
				}
			case "free":
				if p := provider.GetProvider(c.Provider); p != nil && p.HasFree {
					toTest = append(toTest, c)
				}
			case "compatible":
				if strings.HasPrefix(c.Provider, openaiCompatiblePrefix) || strings.HasPrefix(c.Provider, anthropicCompatiblePrefix) {
					toTest = append(toTest, c)
				}
			default:
				writeErr(w, http.StatusBadRequest, "Invalid mode. Use: provider, oauth, free, apikey, compatible, all")
				return
			}
		}
	}

	results := make([]map[string]any, 0, len(toTest))
	for _, conn := range toTest {
		valid := conn.APIKey != "" || conn.AccessToken != ""
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
	passed := 0
	for _, r := range results {
		if r["valid"] == true {
			passed++
		}
	}
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

// GET /api/providers/kilo/free-models
func (s *Server) handleKiloFreeModels(w http.ResponseWriter, r *http.Request) {
	kiloMu.Lock()
	defer kiloMu.Unlock()
	now := time.Now()
	if kiloCache != nil && now.Sub(kiloCacheAt) < kiloCacheTTL {
		writeJSONOK(w, map[string]any{"models": kiloCache, "cached": true})
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(kiloModelsURL)
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
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if kiloCache != nil {
			writeJSONOK(w, map[string]any{"models": kiloCache, "cached": true, "warning": "Kilo API returned " + strconv.Itoa(resp.StatusCode)})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"models": []any{}, "error": "Failed to fetch Kilo models: status " + strconv.Itoa(resp.StatusCode),
		})
		return
	}
	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		if kiloCache != nil {
			writeJSONOK(w, map[string]any{"models": kiloCache, "cached": true, "warning": err.Error()})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"models": []any{}, "error": "Failed to fetch Kilo models: " + err.Error(),
		})
		return
	}
	free := make([]map[string]any, 0)
	for _, m := range payload.Data {
		if isFree, _ := m["isFree"].(bool); isFree {
			ctxLen, _ := asFloat(m["context_length"])
			free = append(free, map[string]any{
				"id": m["id"], "name": m["name"], "isFree": true, "context_length": ctxLen,
			})
		}
	}
	kiloCache = free
	kiloCacheAt = now
	writeJSONOK(w, map[string]any{"models": free, "cached": false})
}

// GET /api/providers/registry — expose full provider registry
func (s *Server) handleProviderRegistry(w http.ResponseWriter, r *http.Request) {
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
		if p.Display.Notice != nil {
			n := map[string]any{}
			if p.Display.Notice.Text != "" {
				n["text"] = p.Display.Notice.Text
			}
			if p.Display.Notice.SignupURL != "" {
				n["signupUrl"] = p.Display.Notice.SignupURL
			}
			if p.Display.Notice.APIKeyURL != "" {
				n["apiKeyUrl"] = p.Display.Notice.APIKeyURL
			}
			if len(n) > 0 {
				entry["notice"] = n
			}
		}
		out = append(out, entry)
	}
	writeJSONOK(w, map[string]any{"registry": out})
}

// DELETE /api/models/alias — delete model alias
func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Alias == "" {
		writeErr(w, http.StatusBadRequest, "alias required")
		return
	}
	if err := s.st.DeleteAlias(req.Alias); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"ok": true})
}

// DELETE /api/models/custom — delete custom model
func (s *Server) handleDeleteCustomModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeErr(w, http.StatusBadRequest, "id required")
		return
	}
	if err := s.st.DeleteCustomModel(req.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}
	writeJSONOK(w, map[string]any{"ok": true})
}
