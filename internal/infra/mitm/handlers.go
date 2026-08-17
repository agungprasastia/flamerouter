// Package mitm provides local HTTPS MITM proxy and certificate generation for developer tools.
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

// HandleRequest executes the wrapped handler function.
func (f FuncHandler) HandleRequest(w http.ResponseWriter, r *http.Request) {
	f(w, r)
}

// PassthroughHandler responds 502 — no tool handler registered.
type PassthroughHandler struct{}

// HandleRequest responds with bad gateway error.
func (PassthroughHandler) HandleRequest(w http.ResponseWriter, _ *http.Request) {
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

// NewModelRewriter creates a new ModelRewriter for a tool.
func NewModelRewriter(name, routerBase, apiKey string) *ModelRewriter {
	return &ModelRewriter{
		name:    name,
		router:  strings.TrimRight(routerBase, "/"),
		apiKey:  apiKey,
		aliases: make(map[string]string),
		mu:      sync.RWMutex{},
	}
}

// SetAlias configures an alias mapping for model rewriting.
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

// SetAliases replaces all configured aliases.
func (m *ModelRewriter) SetAliases(mapp map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.aliases = make(map[string]string, len(mapp))
	for k, v := range mapp {
		m.aliases[k] = v
	}
}

// HandleRequest logs + rewrites model id when configured, then proxies to local router.
func (m *ModelRewriter) HandleRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("[mitm:%s] %s %s", m.name, r.Method, r.URL.Path)

	if r.Method == http.MethodGet && (r.URL.Path == "/_mitm_health" || strings.HasSuffix(r.URL.Path, "/_mitm_health")) {
		w.Header().Set("Content-Type", "application/json")

		if _, writeErr := w.Write([]byte(`{"ok":true,"tool":"` + m.name + `"}`)); writeErr != nil {
			_ = writeErr
		}

		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	if clErr := r.Body.Close(); clErr != nil {
		_ = clErr
	}

	rewritten := m.rewriteBody(body, r.Header.Get("Content-Type"))

	if m.router == "" {
		m.writeNoRouter(w)
		return
	}

	m.proxyToRouter(w, r, rewritten)
}

func (m *ModelRewriter) rewriteBody(body []byte, contentType string) []byte {
	if len(body) == 0 || (!strings.Contains(contentType, "json") && !looksJSON(body)) {
		return body
	}

	var obj map[string]any
	if json.Unmarshal(body, &obj) != nil || obj == nil {
		return body
	}

	m.applyModelAlias(obj)

	if b, err := json.Marshal(obj); err == nil {
		return b
	}

	return body
}

func (m *ModelRewriter) applyModelAlias(obj map[string]any) {
	model, ok := obj["model"].(string)
	if !ok || model == "" {
		return
	}

	m.mu.RLock()
	to, hasAlias := m.aliases[model]
	m.mu.RUnlock()

	if hasAlias && to != "" {
		obj["model"] = to
		log.Printf("[mitm:%s] rewrite model %s -> %s", m.name, model, to)
	}
}

func (m *ModelRewriter) writeNoRouter(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)

	if _, writeErr := w.Write([]byte(`{"error":{"message":"mitm router not configured","type":"mitm_error","tool":"` + m.name + `"}}`)); writeErr != nil {
		_ = writeErr
	}
}

func (m *ModelRewriter) buildProxyRequest(r *http.Request, rewritten []byte) (*http.Request, error) {
	path := r.URL.Path
	if path == "" || !strings.HasPrefix(path, "/v1/") {
		path = "/v1/chat/completions"
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, m.router+path, bytes.NewReader(rewritten))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
	}

	return req, nil
}

func (m *ModelRewriter) proxyToRouter(w http.ResponseWriter, r *http.Request, rewritten []byte) {
	req, err := m.buildProxyRequest(r, rewritten)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		m.writeProxyError(w, err.Error())
		return
	}

	if res == nil || res.Body == nil {
		m.writeProxyError(w, "empty router response")
		return
	}

	defer func() {
		if clErr := res.Body.Close(); clErr != nil {
			_ = clErr
		}
	}()

	copyResponse(w, res)
}

func copyResponse(w http.ResponseWriter, res *http.Response) {
	for k, vals := range res.Header {
		if !strings.EqualFold(k, "Content-Length") {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
	}

	w.WriteHeader(res.StatusCode)

	if _, copyErr := io.Copy(w, res.Body); copyErr != nil {
		_ = copyErr
	}
}

func (m *ModelRewriter) writeProxyError(w http.ResponseWriter, msg string) {
	log.Printf("[mitm:%s] router error: %s", m.name, msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)

	if _, writeErr := w.Write([]byte(`{"error":{"message":"` + escapeJSON(msg) + `","type":"mitm_error"}}`)); writeErr != nil {
		_ = writeErr
	}
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
		{"antigravity", ToolHosts["antigravity"]},
		{"copilot", ToolHosts["copilot"]},
		{"kiro", ToolHosts["kiro"]},
		{"cursor", ToolHosts["cursor"]},
	}
	for _, t := range tools {
		h := NewModelRewriter(t.name, routerBase, apiKey)
		for _, host := range t.hosts {
			out[host] = h
		}
	}

	return out
}
