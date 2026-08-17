package executor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func init() {
	RegisterSpecialized("grok-web", &GrokWebExecutor{
		Base: Base{
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
			Provider: "grok-web",
			BaseURL:  "https://grok.com/rest/app-chat/conversations/new",
		},
	})
}

const (
	grokWebChatAPI   = "https://grok.com/rest/app-chat/conversations/new"
	grokWebUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"
)

type grokWebModelInfo struct {
	GrokModel string
	ModelMode string
}

var grokWebModelMap = map[string]grokWebModelInfo{
	"grok-3":            {GrokModel: "grok-3", ModelMode: "MODEL_MODE_GROK_3"},
	"grok-3-mini":       {GrokModel: "grok-3", ModelMode: "MODEL_MODE_GROK_3_MINI_THINKING"},
	"grok-3-thinking":   {GrokModel: "grok-3", ModelMode: "MODEL_MODE_GROK_3_THINKING"},
	"grok-4":            {GrokModel: "grok-4", ModelMode: "MODEL_MODE_GROK_4"},
	"grok-4-mini":       {GrokModel: "grok-4-mini", ModelMode: "MODEL_MODE_GROK_4_MINI_THINKING"},
	"grok-4-thinking":   {GrokModel: "grok-4", ModelMode: "MODEL_MODE_GROK_4_THINKING"},
	"grok-4-heavy":      {GrokModel: "grok-4", ModelMode: "MODEL_MODE_HEAVY"},
	"grok-4.1-mini":     {GrokModel: "grok-4-1-thinking-1129", ModelMode: "MODEL_MODE_GROK_4_1_MINI_THINKING"},
	"grok-4.1-fast":     {GrokModel: "grok-4-1-thinking-1129", ModelMode: "MODEL_MODE_FAST"},
	"grok-4.1-expert":   {GrokModel: "grok-4-1-thinking-1129", ModelMode: "MODEL_MODE_EXPERT"},
	"grok-4.1-thinking": {GrokModel: "grok-4-1-thinking-1129", ModelMode: "MODEL_MODE_GROK_4_1_THINKING"},
	"grok-4.2":          {GrokModel: "grok-420", ModelMode: "MODEL_MODE_GROK_420"},
	"grok-4.20":         {GrokModel: "grok-420", ModelMode: "MODEL_MODE_GROK_420"},
	"grok-4.20-beta":    {GrokModel: "grok-420", ModelMode: "MODEL_MODE_GROK_420"},
}

// GrokWebExecutor — grok.com web SSO cookie chat (NDJSON → OpenAI SSE/JSON).
type GrokWebExecutor struct{ Base }

func grokRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}

	return hex.EncodeToString(b)
}

func generateStatsigID() string {
	// mirror JS fingerprint noise
	msg := fmt.Sprintf("e:TypeError: Cannot read properties of undefined (reading '%s')", grokRandomHex(5))
	return base64.StdEncoding.EncodeToString([]byte(msg))
}

func extractContentParts(c any) string {
	switch val := c.(type) {
	case string:
		return val
	case []any:
		var parts []string

		for _, p := range val {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}

			if t, okT := pm["type"].(string); okT && t == "text" {
				parts = append(parts, fmt.Sprint(pm["text"]))
			}
		}

		return strings.Join(parts, " ")
	default:
		return ""
	}
}

type grokExtractedMsg struct {
	role string
	text string
}

func extractGrokItem(raw any) (grokExtractedMsg, bool) {
	msg, ok := raw.(map[string]any)
	if !ok {
		return grokExtractedMsg{role: "", text: ""}, false
	}

	role, okRole := msg["role"].(string)
	if !okRole || role == "" {
		role = "user"
	} else if role == "developer" {
		role = "system"
	}

	content := extractContentParts(msg["content"])
	if strings.TrimSpace(content) == "" {
		return grokExtractedMsg{role: "", text: ""}, false
	}

	return grokExtractedMsg{role: role, text: content}, true
}

func parseOpenAIMessages(messages []any) string {
	var items []grokExtractedMsg

	for _, raw := range messages {
		if it, ok := extractGrokItem(raw); ok {
			items = append(items, it)
		}
	}

	if len(items) == 0 {
		return ""
	}

	lastUser := -1

	for i := len(items) - 1; i >= 0; i-- {
		if items[i].role == "user" {
			lastUser = i
			break
		}
	}

	var parts []string

	for i, it := range items {
		if i == lastUser {
			parts = append(parts, it.text)
		} else {
			parts = append(parts, it.role+": "+it.text)
		}
	}

	return strings.Join(parts, "\n\n")
}

func jsonErr(status int, msg, typ, code string) *Result {
	errMap := map[string]any{"message": msg, "type": typ}
	if code != "" {
		errMap["code"] = code
	}

	body := map[string]any{"error": errMap}
	b, _ := json.Marshal(body) // nolint:errcheck

	return &Result{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(b)),
	}
}

func buildGrokWebHeaders(cred Credentials) http.Header {
	traceID := grokRandomHex(16)
	spanID := grokRandomHex(8)
	h := make(http.Header)
	h.Set("Accept", "*/*")
	h.Set("Accept-Language", "en-US,en;q=0.9")
	h.Set("Baggage", "sentry-environment=production,sentry-release=d6add6fb0460641fd482d767a335ef72b9b6abb8,sentry-public_key=b311e0f2690c81f25e2c4cf6d4f7ce1c")
	h.Set("Cache-Control", "no-cache")
	h.Set("Content-Type", "application/json")
	h.Set("Origin", "https://grok.com")
	h.Set("Pragma", "no-cache")
	h.Set("Referer", "https://grok.com/")
	h.Set("Sec-Ch-Ua", `"Google Chrome";v="136", "Chromium";v="136", "Not(A:Brand";v="24"`)
	h.Set("Sec-Ch-Ua-Mobile", "?0")
	h.Set("Sec-Ch-Ua-Platform", `"macOS"`)
	h.Set("Sec-Fetch-Dest", "empty")
	h.Set("Sec-Fetch-Mode", "cors")
	h.Set("Sec-Fetch-Site", "same-origin")
	h.Set("User-Agent", grokWebUserAgent)
	h.Set("x-statsig-id", generateStatsigID())
	h.Set("x-xai-request-id", randomUUID())
	h.Set("traceparent", fmt.Sprintf("00-%s-%s-00", traceID, spanID))

	token := cred.APIKey
	if token == "" {
		token = cred.AccessToken
	}

	if token != "" {
		token = strings.TrimPrefix(token, "sso=")
		h.Set("Cookie", "sso="+token)
	}

	return h
}

func buildGrokWebPayload(info grokWebModelInfo, message string) ([]byte, error) {
	payload := map[string]any{
		"temporary": true, "modelName": info.GrokModel, "modelMode": info.ModelMode, "message": message,
		"fileAttachments": []any{}, "imageAttachments": []any{},
		"disableSearch": false, "enableImageGeneration": false, "returnImageBytes": false,
		"returnRawGrokInXaiRequest": false, "enableImageStreaming": false, "imageGenerationCount": 0,
		"forceConcise": false, "toolOverrides": map[string]any{}, "enableSideBySide": true, "sendFinalMetadata": true,
		"isReasoning": false, "disableTextFollowUps": false, "disableMemory": true,
		"forceSideBySide": false, "isAsyncChat": false, "disableSelfHarmShortCircuit": false,
		"deviceEnvInfo": map[string]any{
			"darkModeEnabled": false, "devicePixelRatio": 2,
			"screenWidth": 2056, "screenHeight": 1329, "viewportWidth": 2056, "viewportHeight": 1083,
		},
	}

	return json.Marshal(payload)
}

func handleGrokWebStatusError(res *Result) *Result {
	status := res.StatusCode
	msg := fmt.Sprintf("Grok returned HTTP %d", status)

	if status == 401 || status == 403 {
		msg = "Grok auth failed — SSO cookie may be expired. Re-paste your sso cookie value from grok.com."
	} else if status == 429 {
		msg = "Grok rate limited. Wait a moment and retry, or rotate cookies."
	}

	DrainBody(res.Body)

	return jsonErr(status, msg, "upstream_error", fmt.Sprintf("HTTP_%d", status))
}

func makeGrokSSEResult(body io.ReadCloser) *Result {
	return &Result{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type":      []string{"text/event-stream"},
			"Cache-Control":     []string{"no-cache"},
			"Connection":        []string{"keep-alive"},
			"X-Accel-Buffering": []string{"no"},
		},
		Body: body,
	}
}

func makeGrokJSONResult(body []byte) *Result {
	return &Result{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func parseGrokRequest(body []byte, model string) (grokWebModelInfo, string, *Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return grokWebModelInfo{GrokModel: "", ModelMode: ""}, "", nil, err
	}

	messages, ok := m["messages"].([]any)
	if !ok || len(messages) == 0 {
		return grokWebModelInfo{GrokModel: "", ModelMode: ""}, "", jsonErr(400, "Missing or empty messages array", "invalid_request", ""), nil
	}

	info, okInfo := grokWebModelMap[model]
	if !okInfo {
		info = grokWebModelMap["grok-4.1-fast"]
	}

	message := parseOpenAIMessages(messages)
	if strings.TrimSpace(message) == "" {
		return grokWebModelInfo{GrokModel: "", ModelMode: ""}, "", jsonErr(400, "Empty query after processing", "invalid_request", ""), nil
	}

	return info, message, nil, nil
}

// Execute performs Grok web completions.
func (e *GrokWebExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	info, message, errRes, err := parseGrokRequest(body, model)
	if err != nil {
		return nil, err
	}

	if errRes != nil {
		return errRes, nil
	}

	payloadBytes, err := buildGrokWebPayload(info, message)
	if err != nil {
		return nil, err
	}

	res, err := e.DoPOST(ctx, grokWebChatAPI, buildGrokWebHeaders(cred), payloadBytes)
	if err != nil {
		return nil, err
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return handleGrokWebStatusError(res), nil
	}

	cid := "chatcmpl-grok-" + randomUUID()[:12]
	created := time.Now().Unix()

	if stream {
		return makeGrokSSEResult(convertGrokNDJSONToSSE(res.Body, model, cid, created)), nil
	}

	jsonBody, err := convertGrokNDJSONToJSON(res.Body, model, cid, created)
	if err != nil {
		return jsonErr(502, err.Error(), "upstream_error", "GROK_ERROR"), nil
	}

	return makeGrokJSONResult(jsonBody), nil
}

type grokChunk struct {
	delta, fullMessage, errorMsg string
	done                         bool
}

func parseGrokNDJSONError(event map[string]any) (grokChunk, bool) {
	if errObj, ok := event["error"].(map[string]any); ok {
		msg, okMsg := errObj["message"].(string)
		if !okMsg || msg == "" {
			msg = fmt.Sprintf("Grok error: %v", errObj["code"])
		}

		return grokChunk{errorMsg: msg, done: true, delta: "", fullMessage: ""}, true
	}

	return grokChunk{errorMsg: "", done: false, delta: "", fullMessage: ""}, false
}

func parseGrokNDJSONResponse(resp map[string]any) (grokChunk, bool) {
	if mr, ok := resp["modelResponse"].(map[string]any); ok {
		if msg, okMsg := mr["message"].(string); okMsg && msg != "" {
			return grokChunk{fullMessage: msg, delta: "", errorMsg: "", done: false}, true
		}

		return grokChunk{fullMessage: "", delta: "", errorMsg: "", done: false}, false
	}

	if tok, ok := resp["token"]; ok && tok != nil {
		return grokChunk{delta: fmt.Sprint(tok), fullMessage: "", errorMsg: "", done: false}, true
	}

	return grokChunk{delta: "", fullMessage: "", errorMsg: "", done: false}, false
}

func parseGrokNDJSONEvent(event map[string]any) (grokChunk, bool) {
	if errChunk, ok := parseGrokNDJSONError(event); ok {
		return errChunk, true
	}

	result, okResult := event["result"].(map[string]any)
	if !okResult {
		return grokChunk{delta: "", fullMessage: "", errorMsg: "", done: false}, false
	}

	resp, okResp := result["response"].(map[string]any)
	if !okResp || resp == nil {
		return grokChunk{delta: "", fullMessage: "", errorMsg: "", done: false}, false
	}

	return parseGrokNDJSONResponse(resp)
}

func readGrokNDJSON(r io.Reader, out chan<- grokChunk) {
	defer close(out)

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if chunk, ok := parseGrokNDJSONEvent(event); ok {
			out <- chunk

			if chunk.done {
				return
			}
		}
	}

	out <- grokChunk{done: true, delta: "", fullMessage: "", errorMsg: ""}
}

func emitGrokStreamChunks(ch <-chan grokChunk, cid, model string, created int64, writeSSE func(map[string]any)) {
	for c := range ch {
		if c.errorMsg != "" {
			writeSSE(map[string]any{
				"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
				"system_fingerprint": nil,
				"choices": []any{map[string]any{
					"index": 0, "delta": map[string]any{"content": "[Error: " + c.errorMsg + "]"},
					"finish_reason": nil, "logprobs": nil,
				}},
			})

			break
		}

		if c.done {
			break
		}

		if c.delta != "" {
			writeSSE(map[string]any{
				"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
				"system_fingerprint": nil,
				"choices": []any{map[string]any{
					"index": 0, "delta": map[string]any{"content": c.delta},
					"finish_reason": nil, "logprobs": nil,
				}},
			})
		}
	}
}

func convertGrokNDJSONToSSE(r io.ReadCloser, model, cid string, created int64) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer func() {
			_ = r.Close()  // nolint:errcheck
			_ = pw.Close() // nolint:errcheck
		}()

		writeSSE := func(obj map[string]any) {
			b, err := json.Marshal(obj)
			if err != nil {
				return
			}

			_, _ = pw.Write([]byte("data: ")) // nolint:errcheck
			_, _ = pw.Write(b)                // nolint:errcheck
			_, _ = pw.Write([]byte("\n\n"))   // nolint:errcheck
		}

		writeSSE(map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"system_fingerprint": nil,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil, "logprobs": nil,
			}},
		})

		ch := make(chan grokChunk, 16)
		go readGrokNDJSON(r, ch)

		emitGrokStreamChunks(ch, cid, model, created, writeSSE)

		writeSSE(map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"system_fingerprint": nil,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{}, "finish_reason": "stop", "logprobs": nil,
			}},
		})

		_, _ = pw.Write([]byte("data: [DONE]\n\n")) // nolint:errcheck
	}()

	return pr
}

func convertGrokNDJSONToJSON(r io.ReadCloser, model, cid string, created int64) ([]byte, error) {
	defer func() {
		_ = r.Close() // nolint:errcheck
	}()

	ch := make(chan grokChunk, 16)
	go readGrokNDJSON(r, ch)

	var full string

	for c := range ch {
		if c.errorMsg != "" {
			return nil, fmt.Errorf("%s", c.errorMsg)
		}

		if c.done {
			break
		}

		if c.fullMessage != "" {
			full = c.fullMessage
		} else if c.delta != "" {
			full += c.delta
		}
	}

	msg := map[string]any{"role": "assistant", "content": full}
	promptTokens := (len(full) + 3) / 4
	completionTokens := promptTokens
	out := map[string]any{
		"id": cid, "object": "chat.completion", "created": created, "model": model,
		"system_fingerprint": nil,
		"choices": []any{map[string]any{
			"index": 0, "message": msg, "finish_reason": "stop", "logprobs": nil,
		}},
		"usage": map[string]any{
			"prompt_tokens": promptTokens, "completion_tokens": completionTokens,
			"total_tokens": promptTokens + completionTokens,
		},
	}

	return json.Marshal(out)
}
