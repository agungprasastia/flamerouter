package executor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"flamerouter/internal/opensse/shared/qoder"
	"github.com/google/uuid"
)

func init() {
	RegisterSpecialized("qoder", NewQoderExecutor(nil))
}

var (
	billingBlockRe = regexp.MustCompile(`"code"\s*:\s*"(112|10605)"`)
	catalogMu      sync.RWMutex
	modelConfigs   = map[string]map[string]any{
		"auto":          {"key": "auto", "is_reasoning": false, "max_output_tokens": 32768, "source": "system"},
		"ultimate":      {"key": "ultimate", "is_reasoning": true, "max_output_tokens": 32768, "source": "system"},
		"performance":   {"key": "performance", "is_reasoning": false, "max_output_tokens": 32768, "source": "system"},
		"efficient":     {"key": "efficient", "is_reasoning": false, "max_output_tokens": 32768, "source": "system"},
		"lite":          {"key": "lite", "is_reasoning": false, "max_output_tokens": 32768, "source": "system"},
		"qmodel":        {"key": "qmodel", "is_reasoning": true, "max_output_tokens": 32768, "source": "system"},
		"qmodel_latest": {"key": "qmodel_latest", "is_reasoning": true, "max_output_tokens": 32768, "source": "system"},
		"dmodel":        {"key": "dmodel", "is_reasoning": true, "max_output_tokens": 32768, "source": "system"},
		"dfmodel":       {"key": "dfmodel", "is_reasoning": false, "max_output_tokens": 32768, "source": "system"},
		"gm51model":     {"key": "gm51model", "is_reasoning": false, "max_output_tokens": 32768, "source": "system"},
		"kmodel":        {"key": "kmodel", "is_reasoning": false, "max_output_tokens": 32768, "source": "system"},
		"mmodel":        {"key": "mmodel", "is_reasoning": false, "max_output_tokens": 32768, "source": "system"},
	}
)

type QoderExecutor struct {
	Base
}

func NewQoderExecutor(client *http.Client) *QoderExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	return &QoderExecutor{
		Base: Base{
			Provider: "qoder",
			Client:   client,
			BaseURL:  qoder.QODER_CHAT_URL_ENCODED,
		},
	}
}

func (e *QoderExecutor) buildURL(cred Credentials) string {
	if e.BaseURL != "" && !strings.Contains(e.BaseURL, "api3.qoder.sh") {
		return e.BaseURL
	}
	raw := cred.APIKey
	if raw == "" {
		raw = cred.AccessToken
	}
	if !strings.HasPrefix(raw, "pt-") && (strings.HasPrefix(raw, "jt-") || strings.HasPrefix(cred.AccessToken, "jt-")) {
		return fmt.Sprintf("%s/algo%s?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1", qoder.QODER_CHAT_BASE_ALT, qoder.QODER_CHAT_SIG_PATH)
	}
	return qoder.QODER_CHAT_URL_ENCODED
}

func extractQoderText(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(v)
	}
}

func normalizeQoderMessages(rawMessages []any) ([]map[string]any, string) {
	if len(rawMessages) == 0 {
		return []map[string]any{}, ""
	}
	var systemParts []string
	var out []map[string]any

	for _, item := range rawMessages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		text := extractQoderText(msg["content"])
		if role == "system" {
			if text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		cloned := make(map[string]any, len(msg))
		for k, v := range msg {
			cloned[k] = v
		}
		cloned["content"] = text
		out = append(out, cloned)
	}
	return out, strings.Join(systemParts, "\n\n")
}

func lastQoderUserText(messages []map[string]any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if role, _ := m["role"].(string); role == "user" {
			if content, _ := m["content"].(string); content != "" {
				return content
			}
		}
	}
	return ""
}

func stableQoderHash(prefix string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(prefix))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func stableQoderChatRecordID(model string, messages []map[string]any, tools any, maxTokens int) string {
	h := sha256.New()
	h.Write([]byte("qoder-record\x00"))
	h.Write([]byte(model))
	for _, m := range messages {
		if role, ok := m["role"].(string); ok && role != "" {
			h.Write([]byte{0})
			h.Write([]byte(role))
		}
		if content, ok := m["content"].(string); ok && content != "" {
			h.Write([]byte{0})
			h.Write([]byte(content))
		}
	}
	if tools != nil {
		h.Write([]byte{0})
		if b, err := json.Marshal(tools); err == nil {
			h.Write(b)
		}
	}
	h.Write([]byte(fmt.Sprintf("\x00mt=%d", maxTokens)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func truncateQoderString(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func getQoderModelConfig(qoderKey string) map[string]any {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	if cfg, ok := modelConfigs[qoderKey]; ok {
		cp := make(map[string]any, len(cfg))
		for k, v := range cfg {
			cp[k] = v
		}
		return cp
	}
	// Fallback dynamic config
	return map[string]any{
		"key":               qoderKey,
		"is_reasoning":      strings.Contains(qoderKey, "qmodel") || strings.Contains(qoderKey, "dmodel"),
		"max_output_tokens": 32768,
		"source":            "system",
	}
}

func isQoderBillingBlock(inner string) bool {
	if inner == "" {
		return false
	}
	lower := strings.ToLower(inner)
	return billingBlockRe.MatchString(inner) || strings.Contains(lower, "pricingurl")
}

func buildQoderRequestBody(model string, body map[string]any, cred Credentials) (string, map[string]any, error) {
	qoderKey := strings.TrimPrefix(model, "qoder/")
	if qoderKey == "" {
		qoderKey = "auto"
	}
	modelConfig := getQoderModelConfig(qoderKey)

	rawMessages, _ := body["messages"].([]any)
	messages, systemText := normalizeQoderMessages(rawMessages)
	tools := body["tools"]

	isReasoning, _ := modelConfig["is_reasoning"].(bool)
	maxOutputTokens := 0
	if mot, ok := modelConfig["max_output_tokens"].(int); ok {
		maxOutputTokens = mot
	}

	maxTokens := 32768
	if maxOutputTokens > 0 {
		maxTokens = maxOutputTokens
	}
	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 && int(mt) < maxTokens {
		maxTokens = int(mt)
	}
	if mct, ok := body["max_completion_tokens"].(float64); ok && mct > 0 && int(mct) < maxTokens {
		maxTokens = int(mct)
	}

	lastUser := lastQoderUserText(messages)
	userID := strPSD(cred, "userId")
	sessionID := stableQoderHash("qoder-session", userID, qoderKey)
	recordID := stableQoderChatRecordID(qoderKey, messages, tools, maxTokens)

	toolsArr, _ := tools.([]any)
	if toolsArr == nil {
		toolsArr = []any{}
	}

	payload := map[string]any{
		"request_id":       uuid.New().String(),
		"request_set_id":   recordID,
		"chat_record_id":   recordID,
		"session_id":       sessionID,
		"stream":           true,
		"chat_task":        "FREE_INPUT",
		"is_reply":         true,
		"is_retry":         false,
		"source":           1,
		"version":          "3",
		"session_type":     "qodercli",
		"agent_id":         "agent_common",
		"task_id":          "common",
		"code_language":    "",
		"chat_prompt":      "",
		"image_urls":       nil,
		"aliyun_user_type": "",
		"system":           systemText,
		"messages":         messages,
		"tools":            toolsArr,
		"parameters":       map[string]any{"max_tokens": maxTokens},
		"chat_context": map[string]any{
			"chatPrompt": "",
			"imageUrls":  nil,
			"extra": map[string]any{
				"context":         []any{},
				"modelConfig":     map[string]any{"key": qoderKey, "is_reasoning": isReasoning},
				"originalContent": lastUser,
			},
			"features": []any{},
			"text":     lastUser,
		},
		"model_config": modelConfig,
		"business": map[string]any{
			"product":  "cli",
			"version":  "1.0.0",
			"type":     "agent",
			"stage":    "start",
			"id":       uuid.New().String(),
			"name":     truncateQoderString(lastUser, 30),
			"begin_at": time.Now().UnixMilli(),
		},
	}

	return qoderKey, payload, nil
}

func wrapQoderSSE(resp *http.Response, model string) *Result {
	pr, pw := io.Pipe()

	go func() {
		defer resp.Body.Close()
		defer pw.Close()

		reader := bufio.NewReader(resp.Body)
		doneEmitted := false

		for {
			line, err := reader.ReadString('\n')
			if len(line) > 0 {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "data:") {
					data := strings.TrimSpace(trimmed[5:])
					if data == "[DONE]" {
						if !doneEmitted {
							_, _ = pw.Write([]byte("data: [DONE]\n\n"))
							doneEmitted = true
						}
						return
					}

					var env struct {
						StatusCodeValue int    `json:"statusCodeValue"`
						Body            string `json:"body"`
					}
					if err := json.Unmarshal([]byte(data), &env); err == nil {
						statusVal := env.StatusCodeValue
						if statusVal == 0 {
							statusVal = 200
						}
						if statusVal != 200 {
							msg := env.Body
							if msg == "" {
								msg = fmt.Sprintf("upstream status %d", statusVal)
							}
							errChunk, _ := json.Marshal(map[string]any{
								"id":      fmt.Sprintf("qoder-error-%d", time.Now().UnixMilli()),
								"object":  "chat.completion.chunk",
								"created": time.Now().Unix(),
								"model":   model,
								"choices": []map[string]any{
									{
										"index": 0,
										"delta": map[string]any{
											"content": fmt.Sprintf("\n[qoder error %d: %s]", statusVal, truncateQoderString(msg, 200)),
										},
										"finish_reason": "stop",
									},
								},
							})
							_, _ = pw.Write([]byte("data: " + string(errChunk) + "\n\n"))
							if !doneEmitted {
								_, _ = pw.Write([]byte("data: [DONE]\n\n"))
								doneEmitted = true
							}
							return
						}

						if env.Body == "[DONE]" {
							if !doneEmitted {
								_, _ = pw.Write([]byte("data: [DONE]\n\n"))
								doneEmitted = true
							}
							return
						}

						if env.Body != "" {
							sanitized := strings.ReplaceAll(strings.ReplaceAll(env.Body, "\r", ""), "\n", "")
							_, _ = pw.Write([]byte("data: " + sanitized + "\n\n"))
						}
					}
				}
			}
			if err != nil {
				if !doneEmitted {
					_, _ = pw.Write([]byte("data: [DONE]\n\n"))
				}
				return
			}
		}
	}()

	hdr := resp.Header.Clone()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	return &Result{
		StatusCode: resp.StatusCode,
		Header:     hdr,
		Body:       pr,
	}
}

func (e *QoderExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	userID := strPSD(cred, "userId")
	if userID == "" {
		errResp := map[string]any{"error": map[string]any{"message": "qoder credential is missing userId; reconnect the account"}}
		b, _ := json.Marshal(errResp)
		hdr := make(http.Header)
		hdr.Set("Content-Type", "application/json")
		return &Result{StatusCode: http.StatusUnauthorized, Header: hdr, Body: io.NopCloser(bytes.NewReader(b))}, nil
	}

	authToken := cred.AccessToken
	if authToken == "" {
		authToken = cred.APIKey
	}
	if authToken == "" {
		errResp := map[string]any{"error": map[string]any{"message": "qoder credential is missing accessToken; reconnect the account"}}
		b, _ := json.Marshal(errResp)
		hdr := make(http.Header)
		hdr.Set("Content-Type", "application/json")
		return &Result{StatusCode: http.StatusUnauthorized, Header: hdr, Body: io.NopCloser(bytes.NewReader(b))}, nil
	}

	var parsedBody map[string]any
	if err := json.Unmarshal(body, &parsedBody); err != nil {
		return nil, err
	}

	qoderKey, payload, err := buildQoderRequestBody(model, parsedBody, cred)
	if err != nil {
		errResp := map[string]any{"error": map[string]any{"message": err.Error()}}
		b, _ := json.Marshal(errResp)
		hdr := make(http.Header)
		hdr.Set("Content-Type", "application/json")
		return &Result{StatusCode: http.StatusBadRequest, Header: hdr, Body: io.NopCloser(bytes.NewReader(b))}, nil
	}

	plainBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	encodedBodyStr := qoder.QoderEncodeBody(plainBody)
	encodedBodyBytes := []byte(encodedBodyStr)

	reqURL := e.buildURL(cred)
	cosyCreds := qoder.CosyCreds{
		UserID:    userID,
		AuthToken: authToken,
		Name:      strPSD(cred, "displayName"),
		Email:     strPSD(cred, "email"),
		MachineID: strPSD(cred, "machineId"),
	}

	cosyHeaders, err := qoder.BuildCosyHeaders(encodedBodyBytes, reqURL, cosyCreds)
	if err != nil {
		errResp := map[string]any{"error": map[string]any{"message": fmt.Sprintf("qoder cosy signing failed: %s", err.Error())}}
		b, _ := json.Marshal(errResp)
		hdr := make(http.Header)
		hdr.Set("Content-Type", "application/json")
		return &Result{StatusCode: http.StatusUnauthorized, Header: hdr, Body: io.NopCloser(bytes.NewReader(b))}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(encodedBodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("X-Model-Key", qoderKey)
	modelSource := "system"
	if mc, ok := payload["model_config"].(map[string]any); ok {
		if s, ok := mc["source"].(string); ok && s != "" {
			modelSource = s
		}
	}
	req.Header.Set("X-Model-Source", modelSource)
	req.Header.Set("Accept-Encoding", "identity")

	for k, v := range cosyHeaders {
		req.Header.Set(k, v)
	}

	resp, err := e.client().Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Result{
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			Body:       resp.Body,
		}, nil
	}

	return wrapQoderSSE(resp, "qoder/"+qoderKey), nil
}
