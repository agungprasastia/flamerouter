package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("codex", &CodexExecutor{
		Base: Base{
			Provider: "codex",
			BaseURL:  "https://chatgpt.com/backend-api/codex/responses",
		},
	})
}

var responsesAllowlist = map[string]bool{
	"model": true, "input": true, "instructions": true, "tools": true,
	"tool_choice": true, "stream": true, "store": true, "reasoning": true,
	"service_tier": true, "include": true, "prompt_cache_key": true,
	"client_metadata": true, "text": true,
}

type CodexExecutor struct {
	Base
}

func (e *CodexExecutor) transform(model string, body map[string]any) map[string]any {
	// Convert system → developer in input
	if input, ok := body["input"].([]any); ok {
		for _, itemRaw := range input {
			item, ok := itemRaw.(map[string]any)
			if !ok {
				continue
			}

			if role, _ := item["role"].(string); role == "system" {
				item["role"] = "developer"
			}
		}
	}
	// Strip non-allowlisted fields
	out := map[string]any{}

	for k, v := range body {
		if responsesAllowlist[k] {
			out[k] = v
		}
	}

	out["model"] = model
	out["stream"] = true

	if _, ok := out["store"]; !ok {
		out["store"] = false
	}

	return out
}

func (e *CodexExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	// If chat format, leave as-is for now — translator should have produced responses shape
	transformed := e.transform(model, m)

	payload, err := json.Marshal(transformed)
	if err != nil {
		return nil, err
	}

	url := e.BaseURL
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		url = base
		if !strings.Contains(base, "responses") {
			url = base + "/responses"
		}
	}

	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "text/event-stream")

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}

	h.Set("OpenAI-Beta", "responses=v1")

	return e.DoPOST(ctx, url, h, payload)
}
