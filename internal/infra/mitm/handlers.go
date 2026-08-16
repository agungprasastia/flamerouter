package mitm

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
)

// Handler handles intercepted HTTPS requests for a hostname.
type Handler interface {
	HandleRequest(w http.ResponseWriter, r *http.Request)
}

// FuncHandler adapts a function to Handler.
type FuncHandler func(w http.ResponseWriter, r *http.Request)

func (f FuncHandler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	f(w, r)
}

// PassthroughHandler responds 502 — no tool handler registered.
type PassthroughHandler struct{}

func (PassthroughHandler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "mitm: no handler for host", http.StatusBadGateway)
}

// ModelRewriter optionally rewrites model id in JSON body.
type ModelRewriter struct {
	aliases map[string]string
	router  string
	apiKey  string
	name    string
	mu      sync.RWMutex
}

func NewModelRewriter(name, routerBase, apiKey string) *ModelRewriter {
	return &ModelRewriter{
		name:    name,
		router:  strings.TrimRight(routerBase, "/"),
		apiKey:  apiKey,
		aliases: make(map[string]string),
	}
}

func (m *ModelRewriter) SetAlias(from, to string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if from == "" {
		return
	}

	if to == "" {
		delete(m.aliases, from)
		return
	}

	m.aliases[from] = to
}

func (m *ModelRewriter) SetAliases(mapp map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.aliases = make(map[string]string, len(mapp))
	for k, v := range mapp {
		m.aliases[k] = v
	}
}

// HandleRequest logs + rewrites model id when configured, then proxies to local router.
// Skeleton: decode JSON, apply alias, POST to router /v1/chat/completions (or passthrough path).
func (m *ModelRewriter) HandleRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("[mitm:%s] %s %s", m.name, r.Method, r.URL.Path)

	if r.Method == http.MethodGet && (r.URL.Path == "/_mitm_health" || strings.HasSuffix(r.URL.Path, "/_mitm_health")) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"tool":"` + m.name + `"}`))

		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	_ = r.Body.Close()

	rewritten := body

	if len(body) > 0 && (strings.Contains(r.Header.Get("Content-Type"), "json") || looksJSON(body)) {
		var obj map[string]any
		if json.Unmarshal(body, &obj) == nil {
			if model, ok := obj["model"].(string); ok && model != "" {
				m.mu.RLock()
				if to, ok := m.aliases[model]; ok && to != "" {
					obj["model"] = to
					log.Printf("[mitm:%s] rewrite model %s -> %s", m.name, model, to)
				}
				m.mu.RUnlock()
			}

			if b, err := json.Marshal(obj); err == nil {
				rewritten = b
			}
		}
	}

	if m.router == "" {
		// log + 502 skeleton when no router configured
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"mitm router not configured","type":"mitm_error","tool":"` + m.name + `"}}`))

		return
	}

	path := r.URL.Path
	if path == "" {
		path = "/v1/chat/completions"
	}
	// map common tool paths to OpenAI chat
	if !strings.HasPrefix(path, "/v1/") {
		path = "/v1/chat/completions"
	}

	url := m.router + path

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(rewritten))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[mitm:%s] router error: %v", m.name, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"` + escapeJSON(err.Error()) + `","type":"mitm_error"}}`))

		return
	}

	defer res.Body.Close()

	for k, vals := range res.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}

		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}

func looksJSON(b []byte) bool {
	b = bytes.TrimSpace(b)
	return len(b) > 0 && (b[0] == '{' || b[0] == '[')
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)

	return s
}

// DefaultToolHandlers returns antigravity/copilot/kiro/cursor handlers (skeleton).
func DefaultToolHandlers(routerBase, apiKey string) map[string]Handler {
	out := make(map[string]Handler)

	tools := []struct {
		name  string
		hosts []string
	}{
		{"antigravity", TOOL_HOSTS["antigravity"]},
		{"copilot", TOOL_HOSTS["copilot"]},
		{"kiro", TOOL_HOSTS["kiro"]},
		{"cursor", TOOL_HOSTS["cursor"]},
	}
	for _, t := range tools {
		h := NewModelRewriter(t.name, routerBase, apiKey)
		for _, host := range t.hosts {
			out[host] = h
		}
	}

	return out
}
