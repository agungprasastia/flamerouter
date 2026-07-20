package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Generic OpenAI-compatible specialized executors with fixed defaults.

func init() {
	// iflow / grok-cli / gcli / gb / grok-web → specialized (iflow.go, grok_cli.go, grok_web.go); aliases in registry.go
	registerSimple("qoder", "https://api2.qoder.ai/v1/chat/completions")
	registerSimple("kimchi", "https://api.kimchi.ai/v1/chat/completions")
	registerSimple("xiaomi-tokenplan", "https://api.xiaomimimo.com/v1/chat/completions")
	registerSimple("mimo-free", "https://api.xiaomimimo.com/v1/chat/completions")
	registerSimple("mmf", "https://api.xiaomimimo.com/v1/chat/completions")
	registerSimple("codebuddy-cn", "https://www.codebuddy.ai/v2/chat/completions")
	registerSimple("perplexity-web", "https://www.perplexity.ai/rest/sse/perplexity_ask")
}

func registerSimple(id, defaultURL string) {
	RegisterSpecialized(id, &SimpleExecutor{Base: Base{Provider: id, BaseURL: defaultURL}, defaultURL: defaultURL})
}

type SimpleExecutor struct {
	Base
	defaultURL string
}

func (e *SimpleExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
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
	url := e.defaultURL
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		url = base
		if !strings.Contains(base, "/chat/") && !strings.Contains(base, "/responses") &&
			!strings.Contains(base, "perplexity") && !strings.Contains(base, "/api/chat") {
			url = base + "/chat/completions"
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
	// provider quirks
	switch e.Provider {
	case "codebuddy-cn":
		h.Set("User-Agent", "CodeBuddy/1.0")
	case "qoder":
		h.Set("User-Agent", "Qoder/1.0")
	case "kimchi":
		h.Set("User-Agent", "Kimchi/1.0")
	case "mimo-free", "mmf", "xiaomi-tokenplan":
		h.Set("User-Agent", "MiMo/1.0")
	case "perplexity-web":
		h.Set("User-Agent", "Mozilla/5.0")
	}
	if stream {
		h.Set("Accept", "text/event-stream")
	}
	return e.DoPOST(ctx, url, h, payload)
}
