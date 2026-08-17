package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const codexPeekBytes = 64 * 1024

var (
	codexSSERetryPatterns           = []string{"server_is_overloaded", "service_unavailable_error"}
	codexSSEAccountFallbackPatterns = []string{"selected model is at capacity", "model_at_capacity"}
	codexSSEUserOutputPatterns      = []string{
		"event: response.output_text.delta",
		"event: response.function_call_arguments.delta",
		`"type":"response.output_text.delta"`,
		`"type":"response.function_call_arguments.delta"`,
	}
)

func init() {
	RegisterSpecialized("codex", &CodexExecutor{
		Base: Base{
			Provider: "codex",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "https://chatgpt.com/backend-api/codex/responses",
			BaseURLs: nil,
		},
	})
}

var responsesAllowlist = map[string]bool{
	"model": true, "input": true, "instructions": true, "tools": true,
	"tool_choice": true, "stream": true, "store": true, "reasoning": true,
	"service_tier": true, "include": true, "prompt_cache_key": true,
	"client_metadata": true, "text": true,
}

// CodexExecutor handles requests for Codex backend responses.
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

			if role, okRole := item["role"].(string); okRole && role == "system" {
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

func resolveCodexAccountID(cred Credentials) string {
	if cred.ProviderSpecificData == nil {
		return ""
	}

	keys := []string{"workspaceId", "chatgptAccountId", "accountId"}
	for _, k := range keys {
		if accID, ok := cred.ProviderSpecificData[k].(string); ok && accID != "" {
			return accID
		}
	}

	return ""
}

func (e *CodexExecutor) buildHeaders(cred Credentials) http.Header {
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
	h.Set("originator", "codex_cli_rs")

	if accID := resolveCodexAccountID(cred); accID != "" {
		h.Set("ChatGPT-Account-ID", accID)
	}

	return h
}

type peekResult struct {
	body       io.ReadCloser
	matchedErr string
	isFallback bool
}

func matchCodexPatterns(lower string) (string, bool, bool) {
	for _, p := range codexSSEAccountFallbackPatterns {
		if strings.Contains(lower, p) {
			return p, true, true
		}
	}

	for _, p := range codexSSERetryPatterns {
		if strings.Contains(lower, p) {
			return p, false, true
		}
	}

	return "", false, false
}

func peekCodexSSEError(rc io.ReadCloser) peekResult {
	if rc == nil {
		return peekResult{body: nil, matchedErr: "", isFallback: false}
	}

	buf := make([]byte, codexPeekBytes)
	n, err := io.ReadFull(rc, buf)
	readData := buf[:n]

	if n == 0 {
		return peekResult{body: rc, matchedErr: "", isFallback: false}
	}

	lower := strings.ToLower(string(readData))
	if matched, fb, ok := matchCodexPatterns(lower); ok {
		DrainBody(rc)

		return peekResult{body: nil, matchedErr: matched, isFallback: fb}
	}

	for _, p := range codexSSEUserOutputPatterns {
		if strings.Contains(lower, p) {
			break
		}
	}

	// Reconstruct reader
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		return peekResult{body: io.NopCloser(bytes.NewReader(readData)), matchedErr: "", isFallback: false}
	}

	return peekResult{
		body:       &combinedReadCloser{prefix: bytes.NewReader(readData), orig: rc},
		matchedErr: "",
		isFallback: false,
	}
}

type combinedReadCloser struct {
	prefix io.Reader
	orig   io.ReadCloser
}

func (c *combinedReadCloser) Read(p []byte) (int, error) {
	if c.prefix != nil {
		n, err := c.prefix.Read(p)
		if err == io.EOF {
			c.prefix = nil
			return c.orig.Read(p)
		}

		return n, err
	}

	return c.orig.Read(p)
}

func (c *combinedReadCloser) Close() error {
	return c.orig.Close()
}

// Execute executes Codex response queries.
func (e *CodexExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, _ bool) (*Result, error) {
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

	h := e.buildHeaders(cred)

	res, err := e.DoPOST(ctx, url, h, payload)
	if err != nil {
		return nil, err
	}

	if res.StatusCode == http.StatusOK {
		peeked := peekCodexSSEError(res.Body)
		if peeked.matchedErr != "" {
			res.StatusCode = http.StatusServiceUnavailable
			res.Body = io.NopCloser(strings.NewReader(`{"error":{"message":"` + peeked.matchedErr + `","type":"server_error","code":"service_unavailable"}}`))
		} else {
			res.Body = peeked.body
		}
	}

	return res, nil
}
