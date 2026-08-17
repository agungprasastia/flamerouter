package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("gemini-cli", &GeminiCLIExecutor{
		currentModel: "",
		Base: Base{
			Provider: "gemini-cli",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "https://cloudcode-pa.googleapis.com/v1internal:generateContent",
			BaseURLs: nil,
		},
	})
}

// GeminiCLIExecutor handles requests through Gemini CLI / Cloud Code API.
type GeminiCLIExecutor struct {
	currentModel string
	Base
}

func (e *GeminiCLIExecutor) buildURL(stream bool) string {
	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}

	base := "https://cloudcode-pa.googleapis.com/v1internal"

	return base + ":" + action
}

func (e *GeminiCLIExecutor) buildHeaders(cred Credentials, stream bool) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}

	h.Set("User-Agent", "GeminiCLI/0.34.0 (darwin; arm64)")
	h.Set("X-Goog-Api-Client", "google-genai-sdk/1.41.0 gl-node/v22.19.0")

	if cred.ProjectID != "" {
		h.Set("X-Goog-User-Project", cred.ProjectID)
	}

	if stream {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}

	return h
}

func (e *GeminiCLIExecutor) transform(model string, body map[string]any, _ Credentials) map[string]any {
	e.currentModel = model
	// already wrapped?
	if body != nil {
		if _, hasReq := body["request"]; hasReq {
			if _, hasModel := body["model"]; hasModel {
				return body
			}
		}
	}

	project := ""
	if p, ok := body["project"].(string); ok {
		project = p
	}
	// strip model from inner if present
	inner := body

	return map[string]any{
		"project": project,
		"model":   model,
		"request": inner,
	}
}

// Execute executes Gemini CLI requests.
func (e *GeminiCLIExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	wrapped := e.transform(model, m, cred)

	payload, err := json.Marshal(wrapped)
	if err != nil {
		return nil, err
	}

	url := e.buildURL(stream)

	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" && strings.Contains(base, "googleapis") {
		action := "generateContent"
		if stream {
			action = "streamGenerateContent?alt=sse"
		}
		// if base already has action, use as-is
		if !strings.Contains(base, "generateContent") {
			url = fmt.Sprintf("%s:%s", base, action)
		} else {
			url = base
		}
	}

	return e.DoPOST(ctx, url, e.buildHeaders(cred, stream), payload)
}
