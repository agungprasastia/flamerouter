package executor

import (
	"context"
	"encoding/json"
	"flamerouter/internal/translator/formats"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("antigravity", &AntigravityExecutor{
		Base: Base{
			Provider: "antigravity",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent",
			BaseURLs: nil,
		},
	})
}

var antigravityBlacklist = []string{
	"output_config", "thinking", "reasoning_effort", "reasoning",
	"enable_thinking", "thinking_budget", "thinkingConfig",
}

// AntigravityExecutor executes Cloud Code / Antigravity Gemini requests.
type AntigravityExecutor struct {
	Base
}

func (e *AntigravityExecutor) stripBlacklisted(obj map[string]any) {
	for _, k := range antigravityBlacklist {
		delete(obj, k)
	}
}

func cleanSingleDecl(dRaw any, isCamel bool) {
	d, okMap := dRaw.(map[string]any)
	if !okMap {
		return
	}

	if params, okP := d["parameters"].(map[string]any); okP {
		d["parameters"] = formats.CleanJSONSchemaForAntigravity(params)
	}

	if isCamel {
		if name, okN := d["name"].(string); okN {
			d["name"] = sanitizeFunctionName(name)
		}
	}
}

func cleanFunctionDeclarations(t map[string]any) {
	if decls, ok := t["functionDeclarations"].([]any); ok {
		for _, dRaw := range decls {
			cleanSingleDecl(dRaw, true)
		}
	}

	if decls, ok := t["function_declarations"].([]any); ok {
		for _, dRaw := range decls {
			cleanSingleDecl(dRaw, false)
		}
	}
}

func cleanAntigravityTools(request map[string]any) {
	tools, ok := request["tools"].([]any)
	if !ok {
		return
	}

	for _, tRaw := range tools {
		if t, okMap := tRaw.(map[string]any); okMap {
			cleanFunctionDeclarations(t)
		}
	}
}

func resolveAntigravityProjectID(body map[string]any, cred Credentials) string {
	if p, ok := body["project"].(string); ok && p != "" {
		return p
	}

	if cred.ProjectID != "" {
		return cred.ProjectID
	}

	if cred.ProviderSpecificData != nil {
		if pid, ok := cred.ProviderSpecificData["projectId"].(string); ok && pid != "" {
			return pid
		}
	}

	return formats.GenerateProjectID()
}

func (e *AntigravityExecutor) transform(model string, body map[string]any, cred Credentials) map[string]any {
	var request map[string]any
	if req, ok := body["request"].(map[string]any); ok {
		request = req
	} else {
		request = body
	}

	e.stripBlacklisted(request)
	e.stripBlacklisted(body)
	cleanAntigravityTools(request)

	project := resolveAntigravityProjectID(body, cred)

	out := map[string]any{
		"project":     project,
		"model":       model,
		"userAgent":   "antigravity",
		"requestType": "agent",
		"request":     request,
	}

	if rid, ok := body["requestId"].(string); ok && rid != "" {
		out["requestId"] = rid
	} else {
		out["requestId"] = formats.GenerateRequestID()
	}

	return out
}

func isSafeIdentChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
		r == '_' || r == '.' || r == ':' || r == '-'
}

func sanitizeIdentRune(r rune, isFirst bool) rune {
	if !isSafeIdentChar(r) {
		return '_'
	}

	if isFirst && r >= '0' && r <= '9' {
		return '_'
	}

	return r
}

func sanitizeFunctionName(name string) string {
	if name == "" {
		return "_unknown"
	}

	var b strings.Builder

	for i, r := range name {
		b.WriteRune(sanitizeIdentRune(r, i == 0))
	}

	s := b.String()
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		s = "_" + s
	}

	if len(s) > 64 {
		s = s[:64]
	}

	return s
}

// Execute executes Antigravity requests.
func (e *AntigravityExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	wrapped := e.transform(model, m, cred)

	payload, err := json.Marshal(wrapped)
	if err != nil {
		return nil, err
	}

	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}

	url := "https://cloudcode-pa.googleapis.com/v1internal:" + action

	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		if !strings.Contains(base, "generateContent") {
			url = base + ":" + action
		} else {
			url = base
		}
	}

	h := make(http.Header)
	h.Set("Content-Type", "application/json")

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}

	h.Set("User-Agent", "antigravity/ide/2.1.1 darwin/arm64")

	if stream {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}

	return e.DoPOST(ctx, url, h, payload)
}
