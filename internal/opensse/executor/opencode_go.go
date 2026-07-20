package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func init() {
	// opencode registered in opencode.go; opencode-go specialized here (messages vs chat path).
	RegisterSpecialized("opencode-go", &OpenCodeGoExecutor{
		Base: Base{
			Provider: "opencode-go",
			BaseURL:  "https://opencode.ai/zen/go/v1",
		},
	})
}

// Models that use Anthropic /messages + x-api-key (parity with 9router opencode-go.js).
var openCodeGoMessagesModels = map[string]bool{
	"minimax-m3":    true,
	"minimax-m2.7":  true,
	"minimax-m2.5":  true,
	"qwen3.7-max":   true,
	"qwen3.7-plus":  true,
	"qwen3.6-plus":  true,
}

const openCodeGoBase = "https://opencode.ai/zen/go/v1"

// OpenCodeGoExecutor routes some models to /messages (Claude), rest to /chat/completions.
type OpenCodeGoExecutor struct{ Base }

func (e *OpenCodeGoExecutor) buildURL(model string, cred Credentials) string {
	base := openCodeGoBase
	if b := strings.TrimRight(cred.BaseURL, "/"); b != "" {
		// strip trailing path segments if client passed full chat URL
		b = strings.TrimSuffix(b, "/chat/completions")
		b = strings.TrimSuffix(b, "/messages")
		base = strings.TrimRight(b, "/")
	}
	if openCodeGoMessagesModels[model] {
		return base + "/messages"
	}
	return base + "/chat/completions"
}

func (e *OpenCodeGoExecutor) buildHeaders(cred Credentials, model string, stream bool) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	key := cred.APIKey
	if key == "" {
		key = cred.AccessToken
	}
	if openCodeGoMessagesModels[model] {
		if key != "" {
			h.Set("x-api-key", key)
		}
		h.Set("anthropic-version", anthropicAPIVersionDefault)
	} else if key != "" {
		h.Set("Authorization", "Bearer "+key)
	}
	if stream {
		h.Set("Accept", "text/event-stream")
	}
	return h
}

func (e *OpenCodeGoExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
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
	return e.DoPOST(ctx, e.buildURL(model, cred), e.buildHeaders(cred, model, stream), payload)
}
