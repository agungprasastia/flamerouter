package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("ollama-local", &OllamaLocalExecutor{
		Base: Base{
			Provider: "ollama-local",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "",
			BaseURLs: nil,
		},
	})
	RegisterSpecialized("ollama", &OllamaLocalExecutor{
		Base: Base{
			Provider: "ollama",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "",
			BaseURLs: nil,
		},
	})
}

// OllamaLocalExecutor executes requests against a local or remote Ollama server.
type OllamaLocalExecutor struct{ Base }

func resolveOllamaHost(cred Credentials) string {
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		return base
	}

	if h := strPSD(cred, "host"); h != "" {
		return strings.TrimRight(h, "/")
	}

	return "http://127.0.0.1:11434"
}

// Execute dispatches the request to Ollama /api/chat.
func (e *OllamaLocalExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
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

	url := resolveOllamaHost(cred) + "/api/chat"
	h := make(http.Header)
	h.Set("Content-Type", "application/json")

	if stream {
		h.Set("Accept", "application/x-ndjson")
	}

	return e.DoPOST(ctx, url, h, payload)
}
