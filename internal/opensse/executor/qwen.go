package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("qwen", &QwenExecutor{
		Base: Base{
			Provider: "qwen",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "https://portal.qwen.ai/v1/chat/completions",
			BaseURLs: nil,
		},
	})
	RegisterSpecialized("dashscope", &QwenExecutor{
		Base: Base{
			Provider: "qwen",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "https://portal.qwen.ai/v1/chat/completions",
			BaseURLs: nil,
		},
	})
}

const qwenUserAgent = "QwenCode/0.12.3 (linux; x64)"

// QwenExecutor handles requests for Qwen and Dashscope.
type QwenExecutor struct{ Base }

func (e *QwenExecutor) buildURL(cred Credentials) string {
	if ru := strPSD(cred, "resourceUrl"); ru != "" {
		host := strings.TrimPrefix(strings.TrimPrefix(ru, "https://"), "http://")
		host = strings.TrimRight(host, "/")

		return "https://" + host + "/v1/chat/completions"
	}

	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		if !strings.Contains(base, "/chat/completions") {
			return base + "/v1/chat/completions"
		}

		return base
	}

	return "https://portal.qwen.ai/v1/chat/completions"
}

func (e *QwenExecutor) transform(body map[string]any) map[string]any {
	// Inject empty system with cache_control first
	sys := map[string]any{
		"role": "system",
		"content": []any{
			map[string]any{"type": "text", "text": "", "cache_control": map[string]any{"type": "ephemeral"}},
		},
	}
	if messages, ok := body["messages"].([]any); ok {
		body["messages"] = append([]any{sys}, messages...)
	} else {
		body["messages"] = []any{sys}
	}
	// thinking + tool_choice sanitize
	thinkingActive := false
	if body["enable_thinking"] == true {
		thinkingActive = true
	}

	if t, ok := body["thinking"].(map[string]any); ok {
		if tt, okTT := t["type"].(string); okTT && tt == "enabled" {
			thinkingActive = true
		}
	}

	if thinkingActive {
		tc := body["tool_choice"]
		if tc == "required" {
			body["tool_choice"] = "auto"
		} else if _, ok := tc.(map[string]any); ok {
			body["tool_choice"] = "auto"
		}
	}

	return body
}

// Execute executes Qwen completion requests.
func (e *QwenExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	m["model"] = model
	m["stream"] = stream
	m = e.transform(m)

	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
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

	h.Set("User-Agent", qwenUserAgent)
	h.Set("X-DashScope-AuthType", "qwen-oauth")
	h.Set("X-DashScope-CacheControl", "enable")
	h.Set("X-DashScope-UserAgent", qwenUserAgent)
	h.Set("X-Stainless-Arch", "x64")
	h.Set("X-Stainless-Lang", "js")
	h.Set("X-Stainless-Os", "Linux")
	h.Set("X-Stainless-Package-Version", "5.11.0")
	h.Set("X-Stainless-Retry-Count", "1")
	h.Set("X-Stainless-Runtime", "node")
	h.Set("X-Stainless-Runtime-Version", "v18.19.1")
	h.Set("Connection", "keep-alive")

	if stream {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}

	return e.DoPOST(ctx, e.buildURL(cred), h, payload)
}
