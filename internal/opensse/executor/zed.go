package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
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
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
		},
	})
	RegisterSpecialized("zd", &ZedExecutor{
		Base: Base{
			Provider: "zed",
			BaseURL:  "https://cloud.zed.dev",
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
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

// ZedExecutor implements proxying to the Zed editor upstream service.
type ZedExecutor struct {
	Base
}

func normalizeZedProvider(value string, model string) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	switch raw {
	case "anthropic":
		return "Anthropic"
	case "openai", "open_ai":
		return "OpenAi"
	case "google", "gemini":
		return "Google"
	case "xai", "x_ai", "x-ai":
		return "XAi"
	}

	m := strings.ToLower(model)

	switch {
	case strings.Contains(m, "claude"):
		return "Anthropic"
	case strings.Contains(m, "gemini"):
		return "Google"
	case strings.Contains(m, "grok") || strings.Contains(m, "xai"):
		return "XAi"
	default:
		return "OpenAi"
	}
}

func buildZedUserAuthHeader(cred Credentials) (string, error) {
	userID := ""

	if cred.ProviderSpecificData != nil {
		if u, ok := cred.ProviderSpecificData["userId"].(string); ok && u != "" {
			userID = u
		}
	}

	token := cred.AccessToken
	if token == "" {
		token = cred.APIKey
	}

	if userID == "" || token == "" {
		return "", fmt.Errorf("zed credential is missing userId or accessToken")
	}

	return fmt.Sprintf("%s %s", userID, token), nil
}

func extractZedTokenFromData(data []byte) (string, error) {
	var parsed struct {
		Token any `json:"token"`
	}

	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}

	switch v := parsed.Token.(type) {
	case string:
		return v, nil
	case []any:
		if len(v) > 0 {
			return fmt.Sprint(v[0]), nil
		}
	case map[string]any:
		if val, ok := v["value"].(string); ok {
			return val, nil
		}
	}

	return "", fmt.Errorf("zed did not return an LLM token")
}

func buildZedTokenRequest(ctx context.Context, cred Credentials, orgID, authHeader string) (*http.Request, error) {
	base := strings.TrimRight(cred.BaseURL, "/")
	if base == "" {
		base = zedDefaultLLMBaseURL
	}

	url := base + "/client/llm_tokens"

	reqBody, err := json.Marshal(map[string]string{"organization_id": orgID})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", authHeader)

	if cred.ProviderSpecificData != nil {
		if sid, ok := cred.ProviderSpecificData["systemId"].(string); ok && sid != "" {
			req.Header.Set("x-zed-system-id", sid)
		}
	}

	return req, nil
}

func (e *ZedExecutor) requestLlmToken(ctx context.Context, cred Credentials, orgID, authHeader string) (string, error) {
	req, err := buildZedTokenRequest(ctx, cred, orgID, authHeader)
	if err != nil {
		return "", err
	}

	resp, err := e.client().Do(req)
	if err != nil {
		return "", err
	}

	if resp == nil || resp.Body == nil {
		return "", fmt.Errorf("nil response from upstream")
	}

	defer func() {
		_ = resp.Body.Close() // nolint:errcheck
	}()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("zed llm_tokens failed: HTTP %d %s", resp.StatusCode, string(bodyBytes))
	}

	return extractZedTokenFromData(bodyBytes)
}

func resolveZedOrgAndUser(cred Credentials) (orgID, userID, tokenSuffix string) {
	orgID = "default"
	userID = ""

	if cred.ProviderSpecificData != nil {
		if o, ok := cred.ProviderSpecificData["organizationId"].(string); ok && o != "" {
			orgID = o
		} else if o, ok := cred.ProviderSpecificData["defaultOrganizationId"].(string); ok && o != "" {
			orgID = o
		}

		if u, ok := cred.ProviderSpecificData["userId"].(string); ok {
			userID = u
		}
	}

	token := cred.AccessToken
	if token == "" {
		token = cred.APIKey
	}

	tokenSuffix = token
	if len(tokenSuffix) > 16 {
		tokenSuffix = tokenSuffix[len(tokenSuffix)-16:]
	}

	return orgID, userID, tokenSuffix
}

func (e *ZedExecutor) fetchLlmToken(ctx context.Context, cred Credentials, forceRefresh bool) (string, error) {
	orgID, userID, tokenSuffix := resolveZedOrgAndUser(cred)
	cacheKey := fmt.Sprintf("%s:%s:%s", userID, orgID, tokenSuffix)

	if !forceRefresh {
		if val, ok := zedTokenCache.Load(cacheKey); ok {
			if entry, ok := val.(zedTokenEntry); ok && time.Now().Before(entry.expiresAt) {
				return entry.token, nil
			}
		}
	}

	authHeader, err := buildZedUserAuthHeader(cred)
	if err != nil {
		return "", err
	}

	llmToken, err := e.requestLlmToken(ctx, cred, orgID, authHeader)
	if err != nil {
		return "", err
	}

	zedTokenCache.Store(cacheKey, zedTokenEntry{
		token:     llmToken,
		expiresAt: time.Now().Add(50 * time.Minute),
	})

	return llmToken, nil
}

func buildZedHeaders(llmToken string) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/x-ndjson, text/event-stream, */*")
	h.Set("User-Agent", "9router/zed")
	h.Set("x-zed-version", zedClientVersion)
	h.Set("x-zed-client-supports-status-messages", "true")
	h.Set("x-zed-client-supports-stream-ended-request-completion-status", "true")
	h.Set("Authorization", "Bearer "+llmToken)

	return h
}

func (e *ZedExecutor) prepareZedRequest(ctx context.Context, cred Credentials, model string, body []byte) (string, http.Header, []byte, string, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return "", nil, nil, "", err
	}

	provider := normalizeZedProvider("", model)
	threadID, _ := m["thread_id"].(string) // nolint:errcheck
	promptID, _ := m["prompt_id"].(string) // nolint:errcheck

	targetFormat := translator.FormatOpenAI
	switch provider {
	case "Anthropic":
		targetFormat = translator.FormatClaude
	case "Google":
		targetFormat = translator.FormatGemini
	case "OpenAi":
		targetFormat = translator.FormatOpenAIResponses
	}

	var providerReq map[string]any
	if targetFormat == translator.FormatOpenAI {
		providerReq = m
		providerReq["model"] = model
		providerReq["stream"] = true
	} else {
		providerReq = translator.DefaultRegistry.TranslateRequest(translator.FormatOpenAI, targetFormat, m, translator.TranslateOptions{
			Model:  model,
			Stream: true,
		})
	}

	payload := map[string]any{
		"thread_id":        threadID,
		"prompt_id":        promptID,
		"provider":         provider,
		"model":            model,
		"provider_request": providerReq,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", nil, nil, "", err
	}

	llmToken, err := e.fetchLlmToken(ctx, cred, false)
	if err != nil {
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
	h := buildZedHeaders(llmToken)

	return url, h, payloadBytes, provider, nil
}

// Execute handles proxying requests to the Zed completions endpoint.
func (e *ZedExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, _ bool) (*Result, error) {
	url, h, payloadBytes, provider, err := e.prepareZedRequest(ctx, cred, model, body)
	if err != nil {
		return nil, err
	}

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

	wrappedBody := wrapZedNDJSONStream(res.Body, provider, model)

	return &Result{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type":  []string{"text/event-stream"},
			"Cache-Control": []string{"no-cache"},
		},
		Body: wrappedBody,
	}, nil
}

func handleZedStatus(status map[string]any, cid, model string, writeSSE func(any)) (shouldBreak, shouldContinue bool) {
	typ, _ := status["type"].(string) // nolint:errcheck
	if typ == "failed" {
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

		return true, false
	}

	if typ == "stream_ended" {
		return true, false
	}

	return false, true
}

func handleZedClaudeEvent(evMap map[string]any, cid, model string, created int64, writeSSE func(any)) bool {
	if _, ok := evMap["choices"]; ok {
		writeSSE(evMap)
		return true
	}

	if evType, ok := evMap["type"].(string); ok && evType == "content_block_delta" {
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

				return true
			}
		}
	}

	return false
}

func processZedPayload(payload map[string]any, provider, cid, model string, created int64, state *concerns.ResponseState, writeSSE func(any)) bool {
	if status, ok := payload["status"].(map[string]any); ok {
		shouldBreak, shouldContinue := handleZedStatus(status, cid, model, writeSSE)
		if shouldBreak {
			return false
		}

		if shouldContinue {
			return true
		}
	}

	event, hasEvent := payload["event"]
	if !hasEvent {
		event = payload
	}

	evMap, ok := event.(map[string]any)
	if !ok {
		writeSSE(event)
		return true
	}

	var sourceFormat string
	switch provider {
	case "Anthropic":
		sourceFormat = translator.FormatClaude
	case "Google":
		sourceFormat = translator.FormatGemini
	case "OpenAi":
		sourceFormat = translator.FormatOpenAIResponses
	default:
		sourceFormat = translator.FormatOpenAI
	}

	if sourceFormat != translator.FormatOpenAI {
		resList := translator.DefaultRegistry.TranslateResponse(sourceFormat, translator.FormatOpenAI, evMap, state)
		if len(resList) > 0 {
			for _, item := range resList {
				writeSSE(item)
			}
			return true
		}
	}

	if handled := handleZedClaudeEvent(evMap, cid, model, created, writeSSE); handled {
		return true
	}

	writeSSE(event)

	return true
}

func wrapZedNDJSONStream(r io.ReadCloser, provider, model string) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = r.Close() }()  // nolint:errcheck
		defer func() { _ = pw.Close() }() // nolint:errcheck

		sc := bufio.NewScanner(r)
		created := time.Now().Unix()
		cid := fmt.Sprintf("chatcmpl-zed-%d", time.Now().UnixMilli())

		var sourceFormat string
		switch provider {
		case "Anthropic":
			sourceFormat = translator.FormatClaude
		case "Google":
			sourceFormat = translator.FormatGemini
		case "OpenAi":
			sourceFormat = translator.FormatOpenAIResponses
		default:
			sourceFormat = translator.FormatOpenAI
		}

		state := concerns.NewResponseState()
		state.Model = model

		writeSSE := func(obj any) {
			b, err := json.Marshal(obj)
			if err != nil {
				return
			}

			_, _ = pw.Write([]byte("data: ")) // nolint:errcheck
			_, _ = pw.Write(b)                // nolint:errcheck
			_, _ = pw.Write([]byte("\n\n"))   // nolint:errcheck
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
				finalList := translator.DefaultRegistry.TranslateResponse(sourceFormat, translator.FormatOpenAI, nil, state)
				for _, item := range finalList {
					writeSSE(item)
				}
				_, _ = pw.Write([]byte("data: [DONE]\n\n")) // nolint:errcheck
				return
			}

			var payload map[string]any
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				continue
			}

			if !processZedPayload(payload, provider, cid, model, created, state, writeSSE) {
				_, _ = pw.Write([]byte("data: [DONE]\n\n")) // nolint:errcheck
				return
			}
		}

		finalList := translator.DefaultRegistry.TranslateResponse(sourceFormat, translator.FormatOpenAI, nil, state)
		for _, item := range finalList {
			writeSSE(item)
		}
		_, _ = pw.Write([]byte("data: [DONE]\n\n")) // nolint:errcheck
	}()

	return pr
}
