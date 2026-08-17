package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

const defaultMitmRouterBase = "http://localhost:20128"

func (s *Server) resolveMitmRouterBase(settings map[string]any) string {
	routerBase := defaultMitmRouterBase

	if s.st != nil {
		if v, errSet := s.st.GetSetting("mitmRouterBaseUrl"); errSet == nil && v != "" {
			routerBase = v
		}
	}

	if v, ok := settings["mitmRouterBaseUrl"].(string); ok && strings.TrimSpace(v) != "" {
		routerBase = strings.TrimSpace(v)
	}

	return routerBase
}

func (s *Server) getAntigravityMitmStatus() (map[string]any, error) {
	m := s.cliTools()

	const toolID = "antigravity-mitm"

	settings, err := m.GetSettings(toolID)
	if err != nil {
		return nil, err
	}

	running, _ := settings["running"].(bool)         //nolint:errcheck
	pid, _ := settings["pid"].(float64)              //nolint:errcheck
	certExists, _ := settings["certExists"].(bool)   //nolint:errcheck
	certTrusted, _ := settings["certTrusted"].(bool) //nolint:errcheck

	var dnsStatus map[string]any
	if dns, ok := settings["dnsStatus"].(map[string]any); ok && dns != nil {
		dnsStatus = dns
	} else {
		dnsStatus = map[string]any{}
	}

	routerBase := s.resolveMitmRouterBase(settings)

	return map[string]any{
		"running":           running,
		"pid":               nilIfZero(pid),
		"certExists":        certExists,
		"certTrusted":       certTrusted,
		"dnsStatus":         dnsStatus,
		"hasCachedPassword": false,
		"isWin":             runtime.GOOS == "windows",
		"needsSudoPassword": runtime.GOOS != "windows",
		"isAdmin":           false,
		"mitmRouterBaseUrl": routerBase,
	}, nil
}

func (s *Server) applyAntigravityMitmAction(action, tool string, settings, dns map[string]any) (map[string]any, error) {
	m := s.cliTools()

	const toolID = "antigravity-mitm"

	if action == "trust-cert" {
		settings["certTrusted"] = true
		settings["dnsStatus"] = dns

		if err := m.PatchSettings(toolID, settings); err != nil {
			return nil, err
		}

		return map[string]any{"success": true, "certTrusted": true}, nil
	}

	if tool == "" || (action != "enable" && action != "disable") {
		return nil, errors.New("action must be enable, disable, or trust-cert")
	}

	dns[tool] = (action == "enable")
	settings["dnsStatus"] = dns

	if err := m.PatchSettings(toolID, settings); err != nil {
		return nil, err
	}

	return map[string]any{"success": true, "dnsStatus": dns}, nil
}

func (s *Server) patchAntigravityMitmAction(action, tool string) (map[string]any, error) {
	m := s.cliTools()

	const toolID = "antigravity-mitm"

	settings, errGet := m.GetSettings(toolID)
	if errGet != nil || settings == nil {
		settings = make(map[string]any)
	}

	dns, okDNS := settings["dnsStatus"].(map[string]any)
	if !okDNS || dns == nil {
		dns = map[string]any{}
	}

	return s.applyAntigravityMitmAction(action, tool, settings, dns)
}

func (s *Server) handleAntigravityMitmGet(w http.ResponseWriter) {
	data, err := s.getAntigravityMitmStatus()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, data)
}

func (s *Server) handleAntigravityMitmPatch(w http.ResponseWriter, r *http.Request) {
	m := s.cliTools()

	const toolID = "antigravity-mitm"

	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if action, okAction := patch["action"].(string); okAction && action != "" {
		tool, _ := patch["tool"].(string) //nolint:errcheck

		res, errAction := s.patchAntigravityMitmAction(action, tool)
		if errAction != nil {
			if errAction.Error() == "action must be enable, disable, or trust-cert" {
				writeErr(w, http.StatusBadRequest, errAction.Error())
				return
			}

			writeErr(w, http.StatusInternalServerError, "db")

			return
		}

		writeJSONOK(w, res)

		return
	}

	if err := m.PatchSettings(toolID, patch); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	settings, errSettings := m.GetSettings(toolID)
	if errSettings != nil {
		settings = map[string]any{}
	}

	writeJSONOK(w, map[string]any{"success": true, "settings": settings})
}

// GET/PATCH /api/cli-tools/antigravity-mitm — status + DNS/settings via KV.
func (s *Server) handleAntigravityMitm(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAntigravityMitmGet(w)
	case http.MethodPatch:
		s.handleAntigravityMitmPatch(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func nilIfZero(f float64) any {
	if f == 0 {
		return nil
	}

	return int(f)
}

type antigravityMitmAliasReq struct {
	Mappings map[string]string `json:"mappings"`
	Tool     string            `json:"tool"`
}

func (s *Server) getAntigravityMitmAlias(w http.ResponseWriter, tool string) {
	m := s.cliTools()

	const key = "antigravity-mitm-alias"

	all, err := m.GetSettings(key)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	if tool != "" {
		if v, ok := all[tool]; ok {
			writeJSONOK(w, map[string]any{"aliases": v})
			return
		}

		writeJSONOK(w, map[string]any{"aliases": map[string]any{}})

		return
	}

	writeJSONOK(w, map[string]any{"aliases": all})
}

func (s *Server) isAntigravityDNSEnabled(tool string) bool {
	m := s.cliTools()

	mitm, errMitm := m.GetSettings("antigravity-mitm")
	if errMitm != nil || mitm == nil {
		return false
	}

	dns, okDNS := mitm["dnsStatus"].(map[string]any)
	if !okDNS || dns == nil {
		return false
	}

	enabled, okEnabled := dns[tool].(bool)
	if !okEnabled {
		return false
	}

	return enabled
}

func sanitizeAliasMappings(raw map[string]string) map[string]string {
	filtered := make(map[string]string, len(raw))

	for alias, model := range raw {
		trimmed := strings.TrimSpace(model)
		if trimmed != "" {
			filtered[alias] = trimmed
		}
	}

	return filtered
}

func (s *Server) patchAntigravityMitmAlias(w http.ResponseWriter, r *http.Request) {
	m := s.cliTools()

	const key = "antigravity-mitm-alias"

	var req antigravityMitmAliasReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.Tool == "" || req.Mappings == nil {
		writeErr(w, http.StatusBadRequest, "tool and mappings required")
		return
	}

	if !s.isAntigravityDNSEnabled(req.Tool) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "DNS must be enabled for " + req.Tool + " before editing model mappings",
		})

		return
	}

	filtered := sanitizeAliasMappings(req.Mappings)
	if err := m.PatchSettings(key, map[string]any{req.Tool: filtered}); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, map[string]any{"success": true, "aliases": filtered})
}

// GET/PATCH /api/cli-tools/antigravity-mitm/alias.
func (s *Server) handleAntigravityMitmAlias(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getAntigravityMitmAlias(w, r.URL.Query().Get("tool"))
	case http.MethodPatch, http.MethodPut:
		s.patchAntigravityMitmAlias(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCoworkMCPToolsGet(w http.ResponseWriter) {
	m := s.cliTools()

	const toolID = "cowork-mcp-tools"

	settings, err := m.GetSettings(toolID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	writeJSONOK(w, settings)
}

func (s *Server) patchToolSettingsAndRespond(w http.ResponseWriter, toolID string, patch map[string]any) {
	m := s.cliTools()

	if err := m.PatchSettings(toolID, patch); err != nil {
		writeErr(w, http.StatusInternalServerError, "db")
		return
	}

	settings, errSettings := m.GetSettings(toolID)
	if errSettings != nil {
		settings = map[string]any{}
	}

	writeJSONOK(w, settings)
}

func (s *Server) handleCoworkMCPToolsPatch(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	s.patchToolSettingsAndRespond(w, "cowork-mcp-tools", patch)
}

func (s *Server) handleCoworkMCPToolsPost(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "url required"})
		return
	}

	writeJSONOK(w, probeMCP(req.URL))
}

// GET/PATCH store; POST probes MCP tools/list (JS parity).
func (s *Server) handleCoworkMCPTools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleCoworkMCPToolsGet(w)
	case http.MethodPatch:
		s.handleCoworkMCPToolsPatch(w, r)
	case http.MethodPost:
		s.handleCoworkMCPToolsPost(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func initMCPClient() *http.Client {
	return &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       8 * time.Second,
	}
}

func sendMCPInitRequest(ctx context.Context, client *http.Client, url string, body []byte, headers map[string]string) (*http.Response, error) {
	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	for k, v := range headers {
		initReq.Header.Set(k, v)
	}

	return client.Do(initReq)
}

func handleMCPInitError(err error) map[string]any {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}

	if strings.Contains(msg, "Timeout") || strings.Contains(msg, "deadline") {
		msg = "timeout"
	}

	return map[string]any{"error": msg, "tools": []any{}}
}

func performMCPInit(ctx context.Context, client *http.Client, url string, headers map[string]string) (string, map[string]any, bool) {
	initBody, errBody := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "flamerouter", "version": "1"},
		},
	})
	if errBody != nil {
		return "", map[string]any{"error": errBody.Error(), "tools": []any{}}, true
	}

	initRes, err := sendMCPInitRequest(ctx, client, url, initBody, headers)
	if err != nil || initRes == nil || initRes.Body == nil {
		return "", handleMCPInitError(err), true
	}

	defer func() { _ = initRes.Body.Close() }() //nolint:errcheck // best effort

	if initRes.StatusCode == http.StatusUnauthorized || initRes.StatusCode == http.StatusForbidden {
		return "", map[string]any{"requiresAuth": true, "tools": []any{}}, true
	}

	if initRes.StatusCode < 200 || initRes.StatusCode >= 300 {
		return "", map[string]any{"error": "init " + http.StatusText(initRes.StatusCode), "tools": []any{}}, true
	}

	sessionID := initRes.Header.Get("mcp-session-id")
	_, _ = io.Copy(io.Discard, initRes.Body) //nolint:errcheck // discarding body

	return sessionID, nil, false
}

func sendMCPInitializedNotification(ctx context.Context, client *http.Client, url string, headers map[string]string) {
	notif, errNotif := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	})
	if errNotif != nil {
		return
	}

	nReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(notif))
	if err != nil {
		return
	}

	for k, v := range headers {
		nReq.Header.Set(k, v)
	}

	if res, errDo := client.Do(nReq); errDo == nil && res != nil && res.Body != nil {
		_ = res.Body.Close() //nolint:errcheck // best effort
	}
}

func parseMCPEventStream(raw []byte) map[string]any {
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

		var obj map[string]any
		if err := json.Unmarshal([]byte(payload), &obj); err != nil {
			continue
		}

		id, okID := obj["id"].(float64)
		if okID && id == 2 {
			if _, has := obj["result"]; has {
				return obj
			}
		}
	}

	return nil
}

func extractMCPTools(parsed map[string]any) []map[string]any {
	toolsOut := []map[string]any{}
	if parsed == nil {
		return toolsOut
	}

	result, okResult := parsed["result"].(map[string]any)
	if !okResult {
		return toolsOut
	}

	tools, okTools := result["tools"].([]any)
	if !okTools {
		return toolsOut
	}

	for _, t := range tools {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}

		name, _ := tm["name"].(string)        //nolint:errcheck
		desc, _ := tm["description"].(string) //nolint:errcheck
		toolsOut = append(toolsOut, map[string]any{"name": name, "description": desc})
	}

	return toolsOut
}

func parseMCPToolsJSON(raw []byte, ct string) map[string]any {
	var parsed map[string]any

	if strings.Contains(ct, "text/event-stream") {
		parsed = parseMCPEventStream(raw)
	} else {
		_ = json.Unmarshal(raw, &parsed) //nolint:errcheck // best effort parse
	}

	return parsed
}

func fetchMCPToolsList(ctx context.Context, client *http.Client, url string, headers map[string]string) map[string]any {
	listBody, errBody := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	if errBody != nil {
		return map[string]any{"error": errBody.Error(), "tools": []any{}}
	}

	listRes, err := sendMCPInitRequest(ctx, client, url, listBody, headers)
	if err != nil || listRes == nil || listRes.Body == nil {
		msg := "unknown error"
		if err != nil {
			msg = err.Error()
		}

		return map[string]any{"error": msg, "tools": []any{}}
	}

	defer func() { _ = listRes.Body.Close() }() //nolint:errcheck // best effort

	if listRes.StatusCode == http.StatusUnauthorized || listRes.StatusCode == http.StatusForbidden {
		return map[string]any{"requiresAuth": true, "tools": []any{}}
	}

	raw, errRead := io.ReadAll(io.LimitReader(listRes.Body, 1<<20))
	if errRead != nil {
		return map[string]any{"error": errRead.Error(), "tools": []any{}}
	}

	ct := listRes.Header.Get("Content-Type")
	parsed := parseMCPToolsJSON(raw, ct)

	return map[string]any{"tools": extractMCPTools(parsed)}
}

func probeMCP(url string) map[string]any {
	client := initMCPClient()
	headers := map[string]string{
		"Content-Type":         "application/json",
		"Accept":               "application/json, text/event-stream",
		"MCP-Protocol-Version": "2025-06-18",
	}

	ctx := context.Background()

	sessionID, earlyRes, done := performMCPInit(ctx, client, url, headers)
	if done {
		return earlyRes
	}

	listHeaders := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		listHeaders[k] = v
	}

	if sessionID != "" {
		listHeaders["mcp-session-id"] = sessionID
	}

	sendMCPInitializedNotification(ctx, client, url, listHeaders)

	return fetchMCPToolsList(ctx, client, url, listHeaders)
}

// registry cache (1h).
var (
	mcpRegMu   sync.Mutex
	mcpRegTS   time.Time
	mcpRegData map[string]any
	mcpRegTTL  = time.Hour
)

// GET /api/cli-tools/cowork-mcp-registry (+ PATCH stores local overrides).
func (s *Server) handleCoworkMCPRegistry(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPatch:
		s.patchCoworkMCPRegistry(w, r)
	case http.MethodGet:
		s.getCoworkMCPRegistry(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) patchCoworkMCPRegistry(w http.ResponseWriter, r *http.Request) {
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	s.patchToolSettingsAndRespond(w, "cowork-mcp-registry", patch)
}

func (s *Server) getCoworkMCPRegistry(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1"

	mcpRegMu.Lock()
	if !force && mcpRegData != nil && time.Since(mcpRegTS) < mcpRegTTL {
		out := map[string]any{"cached": true}
		for k, v := range mcpRegData {
			out[k] = v
		}

		mcpRegMu.Unlock()
		writeJSONOK(w, out)

		return
	}
	mcpRegMu.Unlock()

	servers, err := fetchCoworkMCPRegistry()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error(), "servers": []any{}, "total": 0})
		return
	}

	data := map[string]any{"servers": servers, "total": len(servers)}

	mcpRegMu.Lock()
	mcpRegTS = time.Now()
	mcpRegData = data
	mcpRegMu.Unlock()

	writeJSONOK(w, map[string]any{"cached": false, "servers": servers, "total": len(servers)})
}

func isDirectConnectMCP(u string) bool {
	if u == "" {
		return false
	}

	low := strings.ToLower(u)
	if strings.Contains(low, "mcp.claude.com") {
		return false
	}

	if strings.HasPrefix(low, "https://api.anthropic.com/mcp") {
		return false
	}

	if strings.ContainsAny(u, "<{") {
		return false
	}

	return strings.HasPrefix(low, "https://")
}

type mcpRemoteInfo struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type mcpServerDetail struct {
	Name        string          `json:"name"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Remotes     []mcpRemoteInfo `json:"remotes"`
}

type mcpServerItem struct {
	Meta   map[string]any  `json:"_meta"`
	Server mcpServerDetail `json:"server"`
}

type mcpRegistryPage struct {
	Metadata struct {
		NextCursor string `json:"nextCursor"`
	} `json:"metadata"`
	Servers []mcpServerItem `json:"servers"`
}

func extractToolNames(meta map[string]any) []string {
	toolNames := []string{}

	if tn, ok := meta["toolNames"].([]any); ok {
		for _, x := range tn {
			if str, okStr := x.(string); okStr {
				toolNames = append(toolNames, str)
			}
		}
	}

	return toolNames
}

func extractTitleAndDesc(s mcpServerDetail, meta map[string]any) (string, string) {
	title, okTitle := meta["displayName"].(string)
	if s.Title != "" {
		title = s.Title
	} else if !okTitle || title == "" {
		title = s.Name
	}

	desc, okDesc := meta["oneLiner"].(string)
	if s.Description != "" {
		desc = s.Description
	} else if !okDesc {
		desc = ""
	}

	return title, desc
}

func extractRegistryMeta(item mcpServerItem) (map[string]any, bool) {
	meta := map[string]any{}

	if item.Meta != nil {
		if m, ok := item.Meta["com.anthropic.api/mcp-registry"].(map[string]any); ok {
			meta = m
		}
	}

	if rf, okRF := meta["requiredFields"].([]any); okRF && len(rf) > 0 {
		return nil, false
	}

	return meta, true
}

func buildMCPRegistryEntry(item mcpServerItem) (map[string]any, bool) {
	s := item.Server
	if len(s.Remotes) == 0 || !isDirectConnectMCP(s.Remotes[0].URL) {
		return nil, false
	}

	meta, okMeta := extractRegistryMeta(item)
	if !okMeta {
		return nil, false
	}

	remote := s.Remotes[0]
	transport := "http"

	if remote.Type == "sse" {
		transport = "sse"
	}

	slug, okSlug := meta["slug"].(string)
	if !okSlug || slug == "" {
		slug = s.Name
	}

	title, desc := extractTitleAndDesc(s, meta)
	authless, _ := meta["isAuthless"].(bool) //nolint:errcheck
	icon, _ := meta["iconUrl"].(string)      //nolint:errcheck
	toolNames := extractToolNames(meta)

	entry := map[string]any{
		"name":        s.Name,
		"slug":        slug,
		"title":       title,
		"description": desc,
		"url":         remote.URL,
		"transport":   transport,
		"oauth":       !authless,
		"toolNames":   toolNames,
		"toolCount":   len(toolNames),
	}

	if icon != "" {
		entry["iconUrl"] = icon
	} else {
		entry["iconUrl"] = nil
	}

	return entry, true
}

var errRegistryFetchHalt = errors.New("halt registry fetch")

func fetchCoworkRegistryPage(ctx context.Context, client *http.Client, cursor string) (*mcpRegistryPage, error) {
	const base = "https://api.anthropic.com/mcp-registry/v0/servers"

	const visibility = "commercial,gsuite,gsuite-google"

	u := base + "?limit=500&visibility=" + visibility
	if cursor != "" {
		u += "&cursor=" + cursor
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")

	res, errDo := client.Do(req)
	if errDo != nil || res == nil || res.Body == nil {
		if errDo != nil {
			return nil, errDo
		}

		return nil, errors.New("empty response")
	}

	defer func() { _ = res.Body.Close() }() //nolint:errcheck // best effort

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, errRegistryFetchHalt
	}

	var page mcpRegistryPage
	if errDecode := json.NewDecoder(res.Body).Decode(&page); errDecode != nil {
		return nil, errDecode
	}

	return &page, nil
}

func fetchCoworkMCPRegistry() ([]map[string]any, error) {
	out := []map[string]any{}
	cursor := ""
	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       30 * time.Second,
	}

	ctx := context.Background()

	for i := 0; i < 20; i++ {
		page, err := fetchCoworkRegistryPage(ctx, client, cursor)
		if err != nil {
			if errors.Is(err, errRegistryFetchHalt) {
				break
			}

			return nil, err
		}

		for _, item := range page.Servers {
			if entry, ok := buildMCPRegistryEntry(item); ok {
				out = append(out, entry)
			}
		}

		cursor = page.Metadata.NextCursor
		if cursor == "" {
			break
		}
	}

	// dedupe by url
	seen := map[string]bool{}
	deduped := make([]map[string]any, 0, len(out))

	for _, s := range out {
		u, _ := s["url"].(string) //nolint:errcheck
		if seen[u] {
			continue
		}

		seen[u] = true
		item := s
		deduped = append(deduped, item)
	}

	return deduped, nil
}
