package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"flamerouter/internal/translator/formats"
)

func init() {
	RegisterSpecialized("antigravity", &AntigravityExecutor{
		Base: Base{
			Provider: "antigravity",
			BaseURL:  "https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent",
		},
	})
}

var antigravityBlacklist = []string{
	"output_config", "thinking", "reasoning_effort", "reasoning",
	"enable_thinking", "thinking_budget", "thinkingConfig",
}

type AntigravityExecutor struct {
	Base
}

func (e *AntigravityExecutor) stripBlacklisted(obj map[string]any) {
	for _, k := range antigravityBlacklist {
		delete(obj, k)
	}
}

func (e *AntigravityExecutor) transform(model string, body map[string]any, cred Credentials) map[string]any {
	// Ensure Cloud Code envelope
	var request map[string]any
	if req, ok := body["request"].(map[string]any); ok {
		request = req
	} else {
		request = body
	}
	e.stripBlacklisted(request)
	e.stripBlacklisted(body)

	// Clean tool schemas
	if tools, ok := request["tools"].([]any); ok {
		for _, tRaw := range tools {
			t, ok := tRaw.(map[string]any)
			if !ok {
				continue
			}
			// functionDeclarations
			if decls, ok := t["functionDeclarations"].([]any); ok {
				for _, dRaw := range decls {
					d, ok := dRaw.(map[string]any)
					if !ok {
						continue
					}
					if params, ok := d["parameters"].(map[string]any); ok {
						d["parameters"] = formats.CleanJSONSchemaForAntigravity(params)
					}
					if name, ok := d["name"].(string); ok {
						d["name"] = sanitizeFunctionName(name)
					}
				}
			}
			if decls, ok := t["function_declarations"].([]any); ok {
				for _, dRaw := range decls {
					d, ok := dRaw.(map[string]any)
					if !ok {
						continue
					}
					if params, ok := d["parameters"].(map[string]any); ok {
						d["parameters"] = formats.CleanJSONSchemaForAntigravity(params)
					}
				}
			}
		}
	}

	project := ""
	if p, ok := body["project"].(string); ok && p != "" {
		project = p
	} else if cred.ProjectID != "" {
		project = cred.ProjectID
	} else if cred.ProviderSpecificData != nil {
		if pid, ok := cred.ProviderSpecificData["projectId"].(string); ok && pid != "" {
			project = pid
		}
	}
	if project == "" {
		project = formats.GenerateProjectId()
	}

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
		out["requestId"] = formats.GenerateRequestId()
	}
	return out
}

func sanitizeFunctionName(name string) string {
	if name == "" {
		return "_unknown"
	}
	var b strings.Builder
	for i, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '.' || r == ':' || r == '-'
		if ok {
			if i == 0 && r >= '0' && r <= '9' {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
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
