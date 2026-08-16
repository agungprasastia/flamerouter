package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

const defaultMitmRouterBase = "http://localhost:20128"

// GET/PATCH /api/cli-tools/antigravity-mitm — status + DNS/settings via KV.
func (s *Server) handleAntigravityMitm(w http.ResponseWriter, r *http.Request) {
	m := s.cliTools()

	const toolID = "antigravity-mitm"

	switch r.Method {
	case http.MethodGet:
		settings, err := m.GetSettings(toolID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		running, _ := settings["running"].(bool)
		pid, _ := settings["pid"].(float64)
		certExists, _ := settings["certExists"].(bool)
		certTrusted, _ := settings["certTrusted"].(bool)

		dnsStatus, _ := settings["dnsStatus"].(map[string]any)
		if dnsStatus == nil {
			dnsStatus = map[string]any{}
		}

		routerBase := defaultMitmRouterBase

		if s.st != nil {
			if v, _ := s.st.GetSetting("mitmRouterBaseUrl"); v != "" {
				routerBase = v
			}
		}

		if v, ok := settings["mitmRouterBaseUrl"].(string); ok && strings.TrimSpace(v) != "" {
			routerBase = strings.TrimSpace(v)
		}

		writeJSONOK(w, map[string]any{
			"running": running, "pid": nilIfZero(pid),
			"certExists": certExists, "certTrusted": certTrusted,
			"dnsStatus": dnsStatus, "hasCachedPassword": false,
			"isWin":             runtime.GOOS == "windows",
			"needsSudoPassword": runtime.GOOS != "windows",
			"isAdmin":           false,
			"mitmRouterBaseUrl": routerBase,
		})
	case http.MethodPatch:
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		// action enable/disable/trust-cert stored in dnsStatus map
		if action, _ := patch["action"].(string); action != "" {
			settings, _ := m.GetSettings(toolID)

			dns, _ := settings["dnsStatus"].(map[string]any)
			if dns == nil {
				dns = map[string]any{}
			}

			tool, _ := patch["tool"].(string)
			switch action {
			case "enable":
				if tool != "" {
					dns[tool] = true
				}
			case "disable":
				if tool != "" {
					dns[tool] = false
				}
			case "trust-cert":
				settings["certTrusted"] = true
			default:
				writeErr(w, http.StatusBadRequest, "action must be enable, disable, or trust-cert")
				return
			}

			settings["dnsStatus"] = dns
			if err := m.PatchSettings(toolID, settings); err != nil {
				writeErr(w, http.StatusInternalServerError, "db")
				return
			}

			if action == "trust-cert" {
				writeJSONOK(w, map[string]any{"success": true, "certTrusted": true})
				return
			}

			writeJSONOK(w, map[string]any{"success": true, "dnsStatus": dns})

			return
		}

		if err := m.PatchSettings(toolID, patch); err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		settings, _ := m.GetSettings(toolID)
		writeJSONOK(w, map[string]any{"success": true, "settings": settings})
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

// GET/PATCH /api/cli-tools/antigravity-mitm/alias.
func (s *Server) handleAntigravityMitmAlias(w http.ResponseWriter, r *http.Request) {
	m := s.cliTools()

	const key = "antigravity-mitm-alias"

	switch r.Method {
	case http.MethodGet:
		tool := r.URL.Query().Get("tool")

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
	case http.MethodPatch, http.MethodPut:
		var req struct {
			Tool     string            `json:"tool"`
			Mappings map[string]string `json:"mappings"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}

		if req.Tool == "" || req.Mappings == nil {
			writeErr(w, http.StatusBadRequest, "tool and mappings required")
			return
		}
		// DNS must be enabled for tool (parity)
		mitm, _ := m.GetSettings("antigravity-mitm")
		dns, _ := mitm["dnsStatus"].(map[string]any)
		enabled := false

		if dns != nil {
			if v, ok := dns[req.Tool].(bool); ok {
				enabled = v
			}
		}

		if !enabled {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "DNS must be enabled for " + req.Tool + " before editing model mappings",
			})

			return
		}

		filtered := map[string]string{}

		for alias, model := range req.Mappings {
			model = strings.TrimSpace(model)
			if model != "" {
				filtered[alias] = model
			}
		}

		if err := m.PatchSettings(key, map[string]any{req.Tool: filtered}); err != nil {
			writeErr(w, http.StatusInternalServerError, "db")
			return
		}

		writeJSONOK(w, map[string]any{"success": true, "aliases": filtered})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GET/PATCH store; POST probes MCP tools/list (JS parity).
func (s *Server) handleCoworkMCPTools(w http.ResponseWriter, r *http.Request) {
	m := s.cliTools()

	const toolID = "cowork-mcp-tools"

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

		settings, _ := m.GetSettings(toolID)
		writeJSONOK(w, settings)
	case http.MethodPost:
		var req struct {
			URL string `json:"url"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "url required"})
			return
		}

		writeJSONOK(w, probeMCP(req.URL))
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func probeMCP(url string) map[string]any {
	client := &http.Client{Timeout: 8 * time.Second}
	headers := map[string]string{
		"Content-Type":         "application/json",
		"Accept":               "application/json, text/event-stream",
		"MCP-Protocol-Version": "2025-06-18",
	}
	initBody, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "flamerouter", "version": "1"},
		},
	})

	initReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(initBody))
	if err != nil {
		return map[string]any{"error": err.Error(), "tools": []any{}}
	}

	for k, v := range headers {
		initReq.Header.Set(k, v)
	}

	initRes, err := client.Do(initReq)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "Timeout") || strings.Contains(msg, "deadline") {
			msg = "timeout"
		}

		return map[string]any{"error": msg, "tools": []any{}}
	}

	defer initRes.Body.Close()

	if initRes.StatusCode == 401 || initRes.StatusCode == 403 {
		return map[string]any{"requiresAuth": true, "tools": []any{}}
	}

	if initRes.StatusCode < 200 || initRes.StatusCode >= 300 {
		return map[string]any{"error": "init " + http.StatusText(initRes.StatusCode), "tools": []any{}}
	}

	sessionID := initRes.Header.Get("mcp-session-id")
	_, _ = io.Copy(io.Discard, initRes.Body)

	listHeaders := map[string]string{}
	for k, v := range headers {
		listHeaders[k] = v
	}

	if sessionID != "" {
		listHeaders["mcp-session-id"] = sessionID
	}
	// notifications/initialized
	notif, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}})

	nReq, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(notif))
	for k, v := range listHeaders {
		nReq.Header.Set(k, v)
	}

	if res, err := client.Do(nReq); err == nil {
		res.Body.Close()
	}

	listBody, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})

	listReq, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(listBody))
	for k, v := range listHeaders {
		listReq.Header.Set(k, v)
	}

	listRes, err := client.Do(listReq)
	if err != nil {
		return map[string]any{"error": err.Error(), "tools": []any{}}
	}

	defer listRes.Body.Close()

	if listRes.StatusCode == 401 || listRes.StatusCode == 403 {
		return map[string]any{"requiresAuth": true, "tools": []any{}}
	}

	ct := listRes.Header.Get("Content-Type")
	raw, _ := io.ReadAll(io.LimitReader(listRes.Body, 1<<20))

	var parsed map[string]any

	if strings.Contains(ct, "text/event-stream") {
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "data:") {
				continue
			}

			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

			var obj map[string]any

			if json.Unmarshal([]byte(payload), &obj) == nil {
				if id, ok := obj["id"].(float64); ok && id == 2 {
					if _, has := obj["result"]; has {
						parsed = obj
						break
					}
				}
			}
		}
	} else {
		_ = json.Unmarshal(raw, &parsed)
	}

	toolsOut := []map[string]any{}

	if parsed != nil {
		if result, ok := parsed["result"].(map[string]any); ok {
			if tools, ok := result["tools"].([]any); ok {
				for _, t := range tools {
					tm, ok := t.(map[string]any)
					if !ok {
						continue
					}

					name, _ := tm["name"].(string)
					desc, _ := tm["description"].(string)
					toolsOut = append(toolsOut, map[string]any{"name": name, "description": desc})
				}
			}
		}
	}

	return map[string]any{"tools": toolsOut}
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
	m := s.cliTools()

	const toolID = "cowork-mcp-registry"

	switch r.Method {
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

		settings, _ := m.GetSettings(toolID)
		writeJSONOK(w, settings)
	case http.MethodGet:
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
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
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

func fetchCoworkMCPRegistry() ([]map[string]any, error) {
	const base = "https://api.anthropic.com/mcp-registry/v0/servers"

	const visibility = "commercial,gsuite,gsuite-google"

	out := []map[string]any{}
	cursor := ""
	client := &http.Client{Timeout: 30 * time.Second}

	for i := 0; i < 20; i++ {
		u := base + "?limit=500&visibility=" + visibility
		if cursor != "" {
			u += "&cursor=" + cursor
		}

		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Accept", "application/json")

		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if res.StatusCode < 200 || res.StatusCode >= 300 {
			res.Body.Close()
			break
		}

		var j struct {
			Servers []struct {
				Server struct {
					Name        string `json:"name"`
					Title       string `json:"title"`
					Description string `json:"description"`
					Remotes     []struct {
						URL  string `json:"url"`
						Type string `json:"type"`
					} `json:"remotes"`
				} `json:"server"`
				Meta map[string]any `json:"_meta"`
			} `json:"servers"`
			Metadata struct {
				NextCursor string `json:"nextCursor"`
			} `json:"metadata"`
		}

		err = json.NewDecoder(res.Body).Decode(&j)
		res.Body.Close()

		if err != nil {
			return nil, err
		}

		for _, item := range j.Servers {
			s := item.Server
			if len(s.Remotes) == 0 || !isDirectConnectMCP(s.Remotes[0].URL) {
				continue
			}

			meta := map[string]any{}

			if item.Meta != nil {
				if m, ok := item.Meta["com.anthropic.api/mcp-registry"].(map[string]any); ok {
					meta = m
				}
			}

			if rf, ok := meta["requiredFields"].([]any); ok && len(rf) > 0 {
				continue
			}

			remote := s.Remotes[0]
			transport := "http"

			if remote.Type == "sse" {
				transport = "sse"
			}

			toolNames := []string{}

			if tn, ok := meta["toolNames"].([]any); ok {
				for _, x := range tn {
					if str, ok := x.(string); ok {
						toolNames = append(toolNames, str)
					}
				}
			}

			slug, _ := meta["slug"].(string)
			if slug == "" {
				slug = s.Name
			}

			title := s.Title
			if title == "" {
				if dn, ok := meta["displayName"].(string); ok {
					title = dn
				} else {
					title = s.Name
				}
			}

			desc := s.Description
			if desc == "" {
				if ol, ok := meta["oneLiner"].(string); ok {
					desc = ol
				}
			}

			authless, _ := meta["isAuthless"].(bool)
			icon, _ := meta["iconUrl"].(string)
			entry := map[string]any{
				"name": s.Name, "slug": slug, "title": title, "description": desc,
				"url": remote.URL, "transport": transport, "oauth": !authless,
				"toolNames": toolNames, "toolCount": len(toolNames),
			}

			if icon != "" {
				entry["iconUrl"] = icon
			} else {
				entry["iconUrl"] = nil
			}

			out = append(out, entry)
		}

		cursor = j.Metadata.NextCursor
		if cursor == "" {
			break
		}
	}
	// dedupe by url
	seen := map[string]bool{}
	deduped := make([]map[string]any, 0, len(out))

	for _, s := range out {
		u, _ := s["url"].(string)
		if seen[u] {
			continue
		}

		seen[u] = true

		deduped = append(deduped, s)
	}

	return deduped, nil
}
