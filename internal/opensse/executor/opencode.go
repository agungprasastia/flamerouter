package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("opencode", &OpenCodeExecutor{Base: Base{Provider: "opencode", BaseURL: "https://opencode.ai/zen/v1/chat/completions"}})
}

type OpenCodeExecutor struct{ Base }

func (e *OpenCodeExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	m["model"] = model
	m["stream"] = stream
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	url := e.BaseURL
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		url = base
		if !strings.Contains(base, "/chat/completions") {
			url = base + "/chat/completions"
		}
	}
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	tok := cred.APIKey
	if tok == "" {
		tok = cred.AccessToken
	}
	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}
	if stream {
		h.Set("Accept", "text/event-stream")
	}
	return e.DoPOST(ctx, url, h, payload)
}
