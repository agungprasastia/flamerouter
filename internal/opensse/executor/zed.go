package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func init() {
	RegisterSpecialized("zed", &ZedExecutor{
		Base: Base{
			Provider: "zed",
			BaseURL:  "https://cloud.zed.dev",
		},
	})
	RegisterSpecialized("zd", &ZedExecutor{
		Base: Base{
			Provider: "zed",
			BaseURL:  "https://cloud.zed.dev",
		},
	})
}

const (
	zedDefaultLLMBaseURL = "https://cloud.zed.dev"
	zedClientVersion     = "0.200.0"
)

var zedTokenCache sync.Map // key: string -> zedTokenEntry

type zedTokenEntry struct {
	expiresAt time.Time
	token     string
}

type ZedExecutor struct {
	Base
}

func normalizeZedProvider(value string, model string) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "anthropic" {
		return "Anthropic"
	}

	if raw == "openai" || raw == "open_ai" {
		return "OpenAi"
	}

	if raw == "google" || raw == "gemini" {
		return "Google"
	}

	if raw == "xai" || raw == "x_ai" || raw == "x-ai" {
		return "XAi"
	}

	m := strings.ToLower(model)
	if strings.Contains(m, "claude") {
		return "Anthropic"
	}

	if strings.Contains(m, "gemini") {
		return "Google"
	}

	if strings.Contains(m, "grok") || strings.Contains(m, "xai") {
		return "XAi"
	}

	return "OpenAi"
}

func buildZedUserAuthHeader(cred Credentials) (string, error) {
	userId := ""

	if cred.ProviderSpecificData != nil {
		if u, ok := cred.ProviderSpecificData["userId"].(string); ok && u != "" {
			userId = u
		}
	}

	token := cred.AccessToken
	if token == "" {
		token = cred.APIKey
	}

	if userId == "" || token == "" {
		return "", fmt.Errorf("Zed credential is missing userId or accessToken")
	}

	return fmt.Sprintf("%s %s", userId, token), nil
}

func (e *ZedExecutor) fetchLlmToken(ctx context.Context, cred Credentials, forceRefresh bool) (string, error) {
	orgId := ""
	userId := ""

	if cred.ProviderSpecificData != nil {
		if o, ok := cred.ProviderSpecificData["organizationId"].(string); ok && o != "" {
			orgId = o
		} else if o, ok := cred.ProviderSpecificData["defaultOrganizationId"].(string); ok && o != "" {
			orgId = o
		}

		if u, ok := cred.ProviderSpecificData["userId"].(string); ok {
			userId = u
		}
	}

	if orgId == "" {
		orgId = "default"
	}

	token := cred.AccessToken
	if token == "" {
		token = cred.APIKey
	}

	tokenSuffix := token
	if len(tokenSuffix) > 16 {
		tokenSuffix = tokenSuffix[len(tokenSuffix)-16:]
	}

	cacheKey := fmt.Sprintf("%s:%s:%s", userId, orgId, tokenSuffix)

	if !forceRefresh {
		if val, ok := zedTokenCache.Load(cacheKey); ok {
			entry := val.(zedTokenEntry)
			if time.Now().Before(entry.expiresAt) {
				return entry.token, nil
			}
		}
	}

	authHeader, err := buildZedUserAuthHeader(cred)
	if err != nil {
		return "", err
	}

	base := strings.TrimRight(cred.BaseURL, "/")
	if base == "" {
		base = zedDefaultLLMBaseURL
	}

	url := base + "/client/llm_tokens"

	reqBody, _ := json.Marshal(map[string]string{"organization_id": orgId})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", authHeader)

	if cred.ProviderSpecificData != nil {
		if sid, ok := cred.ProviderSpecificData["systemId"].(string); ok && sid != "" {
			req.Header.Set("x-zed-system-id", sid)
		}
	}

	resp, err := e.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("Zed llm_tokens failed: HTTP %d %s", resp.StatusCode, string(bodyBytes))
	}

	var data struct {
		Token any `json:"token"`
	}

	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return "", err
	}

	llmToken := ""
	switch v := data.Token.(type) {
	case string:
		llmToken = v
	case []any:
		if len(v) > 0 {
			llmToken = fmt.Sprint(v[0])
		}
	case map[string]any:
		if val, ok := v["value"].(string); ok {
			llmToken = val
		}
	}

	if llmToken == "" {
		return "", fmt.Errorf("Zed did not return an LLM token")
	}

	zedTokenCache.Store(cacheKey, zedTokenEntry{
		token:     llmToken,
		expiresAt: time.Now().Add(50 * time.Minute),
	})

	return llmToken, nil
}

func (e *ZedExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	provider := normalizeZedProvider("", model)
	threadID, _ := m["thread_id"].(string)
	promptID, _ := m["prompt_id"].(string)

	payload := map[string]any{
		"thread_id":        threadID,
		"prompt_id":        promptID,
		"provider":         provider,
		"model":            model,
		"provider_request": m,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	llmToken, err := e.fetchLlmToken(ctx, cred, false)
	if err != nil {
		// If fetchLlmToken fails, fallback to direct token
		llmToken = cred.AccessToken
		if llmToken == "" {
			llmToken = cred.APIKey
		}
	}

	base := strings.TrimRight(cred.BaseURL, "/")
	if base == "" {
		base = zedDefaultLLMBaseURL
	}

	url := base + "/completions"

	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/x-ndjson, text/event-stream, */*")
	h.Set("User-Agent", "9router/zed")
	h.Set("x-zed-version", zedClientVersion)
	h.Set("x-zed-client-supports-status-messages", "true")
	h.Set("x-zed-client-supports-stream-ended-request-completion-status", "true")
	h.Set("Authorization", "Bearer "+llmToken)

	res, err := e.DoPOST(ctx, url, h, payloadBytes)
	if err != nil {
		return nil, err
	}

	// Retry on 401
	if res.StatusCode == 401 {
		DrainBody(res.Body)

		refreshedToken, refreshErr := e.fetchLlmToken(ctx, cred, true)
		if refreshErr == nil && refreshedToken != "" {
			h.Set("Authorization", "Bearer "+refreshedToken)

			res, err = e.DoPOST(ctx, url, h, payloadBytes)
			if err != nil {
				return nil, err
			}
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res, nil
	}

	wrappedBody := wrapZedNDJSONStream(res.Body, model)

	return &Result{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type":  []string{"text/event-stream"},
			"Cache-Control": []string{"no-cache"},
		},
		Body: wrappedBody,
	}, nil
}

func wrapZedNDJSONStream(r io.ReadCloser, model string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer r.Close()
		defer pw.Close()

		sc := bufio.NewScanner(r)
		created := time.Now().Unix()
		cid := fmt.Sprintf("chatcmpl-zed-%d", time.Now().UnixMilli())

		writeSSE := func(obj any) {
			b, _ := json.Marshal(obj)
			_, _ = pw.Write([]byte("data: "))
			_, _ = pw.Write(b)
			_, _ = pw.Write([]byte("\n\n"))
		}

		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}

			if strings.HasPrefix(line, "data:") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}

			if line == "[DONE]" {
				break
			}

			var payload map[string]any
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				continue
			}

			// check status
			if status, ok := payload["status"].(map[string]any); ok {
				if typ, _ := status["type"].(string); typ == "failed" {
					msg := fmt.Sprintf("%v", status["message"])
					writeSSE(map[string]any{
						"id":     cid,
						"object": "chat.completion.chunk",
						"model":  model,
						"choices": []any{map[string]any{
							"index": 0,
							"delta": map[string]any{
								"content": fmt.Sprintf("[Zed error] %s", msg),
							},
							"finish_reason": "stop",
						}},
					})

					break
				}

				if typ, _ := status["type"].(string); typ == "stream_ended" {
					break
				}

				continue
			}

			// check event
			event, hasEvent := payload["event"]
			if !hasEvent {
				event = payload
			}

			// If event is already an OpenAI chunk or object, forward or unwrap
			if evMap, ok := event.(map[string]any); ok {
				if _, ok := evMap["choices"]; ok {
					writeSSE(evMap)
					continue
				}
				// Claude / Anthropic event unwrapping:
				// content_block_delta -> delta.text
				if evType, _ := evMap["type"].(string); evType == "content_block_delta" {
					if delta, ok := evMap["delta"].(map[string]any); ok {
						if text, ok := delta["text"].(string); ok && text != "" {
							writeSSE(map[string]any{
								"id":      cid,
								"object":  "chat.completion.chunk",
								"created": created,
								"model":   model,
								"choices": []any{map[string]any{
									"index":         0,
									"delta":         map[string]any{"content": text},
									"finish_reason": nil,
								}},
							})

							continue
						}
					}
				}
			}

			// Generic chunk forward
			writeSSE(event)
		}

		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()

	return pr
}
