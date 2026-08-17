package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func init() {
	RegisterSpecialized("trae", &TraeExecutor{
		Base: Base{
			Provider: "trae",
			BaseURL:  "https://core-normal.trae.ai/api/remote/v1",
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
		},
	})
	RegisterSpecialized("tr", &TraeExecutor{
		Base: Base{
			Provider: "trae",
			BaseURL:  "https://core-normal.trae.ai/api/remote/v1",
			Client:   nil,
			Headers:  nil,
			BaseURLs: nil,
		},
	})
}

const (
	traeDefaultBaseURL = "https://core-normal.trae.ai/api/remote/v1"
	traeUserAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
)

type TraeExecutor struct {
	Base
}

func (e *TraeExecutor) base(cred Credentials) string {
	b := strings.TrimRight(cred.BaseURL, "/")
	if b == "" {
		b = e.BaseURL
	}

	if b == "" {
		b = traeDefaultBaseURL
	}

	return b
}

func (e *TraeExecutor) buildHeaders(cred Credentials, stream bool) http.Header {
	token := cred.AccessToken
	if token == "" {
		token = cred.APIKey
	}

	psd := cred.ProviderSpecificData
	appLang := "en"
	userRegion := "US"

	if psd != nil {
		if l, ok := psd["appLanguage"].(string); ok && l != "" {
			appLang = l
		}

		if r, ok := psd["userRegion"].(string); ok && r != "" {
			userRegion = r
		}
	}

	h := make(http.Header)
	h.Set("Authorization", "Cloud-IDE-JWT "+token)
	h.Set("Content-Type", "application/json")
	h.Set("X-Trae-Client-Type", "web")
	h.Set("X-Preferenced-Language", appLang)
	h.Set("x-user-region", userRegion)
	h.Set("Referer", "https://solo.trae.ai/")
	h.Set("User-Agent", traeUserAgent)

	if stream {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}

	return h
}

type traeModeInfo struct {
	mode      string
	strategy  string
	modelName string
}

func resolveTraeMode(model string) traeModeInfo {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "work" || m == "auto-work" || m == "solo-work" {
		return traeModeInfo{mode: "work", strategy: "auto", modelName: ""}
	}

	auto := m == "" || m == "auto"
	strategy := "manual"
	modelName := model

	if auto {
		strategy = "auto"
		modelName = ""
	}

	return traeModeInfo{mode: "code", strategy: strategy, modelName: modelName}
}

func flattenTraeQuery(messages []any) string {
	var parts []string

	for _, mRaw := range messages {
		msg, ok := mRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)

		content := ""
		switch c := msg["content"].(type) {
		case string:
			content = c
		case []any:
			var sub []string

			for _, p := range c {
				if s, ok := p.(string); ok {
					sub = append(sub, s)
				} else if pm, ok := p.(map[string]any); ok {
					if t, ok := pm["text"].(string); ok {
						sub = append(sub, t)
					}
				}
			}

			content = strings.Join(sub, "")
		}

		if role == "system" {
			parts = append(parts, fmt.Sprintf("[System]\n%s", content))
		} else if role == "assistant" {
			parts = append(parts, fmt.Sprintf("[Assistant]\n%s", content))
		} else {
			parts = append(parts, content)
		}
	}

	typedBlocks := []map[string]any{
		{
			"type": "text",
			"data": map[string]any{
				"content": strings.Join(parts, "\n\n"),
			},
		},
	}
	b, _ := json.Marshal(typedBlocks)

	return string(b)
}

func buildTraeCommonParams(psd map[string]any, mode string) string {
	appLang := "en"
	appVer := "1.0.0.1229"
	webID := ""
	userIdentity := "Free"
	bizUserID := ""
	userUniqueID := ""
	scope := "marscode-us"
	tenant := "marscode"
	region := "US-East"
	aiRegion := "US-East"

	if psd != nil {
		if v, ok := psd["appLanguage"].(string); ok && v != "" {
			appLang = v
		}

		if v, ok := psd["appVersion"].(string); ok && v != "" {
			appVer = v
		}

		if v, ok := psd["webId"].(string); ok {
			webID = v
		}

		if v, ok := psd["userIdentity"].(string); ok && v != "" {
			userIdentity = v
		}

		if v, ok := psd["bizUserId"].(string); ok {
			bizUserID = v
		}

		if v, ok := psd["userUniqueId"].(string); ok {
			userUniqueID = v
		}

		if v, ok := psd["scope"].(string); ok && v != "" {
			scope = v
		}

		if v, ok := psd["tenant"].(string); ok && v != "" {
			tenant = v
		}

		if v, ok := psd["region"].(string); ok && v != "" {
			region = v
		}

		if v, ok := psd["aiRegion"].(string); ok && v != "" {
			aiRegion = v
		}
	}

	cp := map[string]any{
		"language":        "en-us",
		"app_language":    appLang,
		"quality":         "stable",
		"app_version":     appVer,
		"web_id":          webID,
		"user_identity":   userIdentity,
		"is_freshman":     "0",
		"biz_user_id":     bizUserID,
		"user_unique_id":  userUniqueID,
		"scope":           scope,
		"tenant":          tenant,
		"region":          region,
		"aiRegion":        aiRegion,
		"is_privacy_mode": 0,
		"privacy_mode":    "off",
		"solo_chat_mode":  mode,
	}
	b, _ := json.Marshal(cp)

	return string(b)
}

func (e *TraeExecutor) createSession(ctx context.Context, headers http.Header, query, model string, cred Credentials) (sessionID, messageID string, err error) {
	modeInfo := resolveTraeMode(model)
	bodyObj := map[string]any{
		"mode":           modeInfo.mode,
		"environment_id": "default",
		"initial_message": map[string]any{
			"chat_session_id":          "",
			"content":                  []any{},
			"query":                    query,
			"model_name":               modeInfo.modelName,
			"agent_type":               "solo_agent_remote",
			"model_selection_strategy": modeInfo.strategy,
			"common_params":            buildTraeCommonParams(cred.ProviderSpecificData, modeInfo.mode),
		},
		"env":                 "remote",
		"auto_create_project": false,
		"origin":              "web",
	}

	reqBytes, err := json.Marshal(bodyObj)
	if err != nil {
		return "", "", err
	}

	url := e.base(cred) + "/chat_sessions"

	res, err := e.DoPOST(ctx, url, headers, reqBytes)
	if err != nil {
		return "", "", err
	}

	defer res.Body.Close()

	bodyBytes, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", "", fmt.Errorf("[%d] %s", res.StatusCode, string(bodyBytes))
	}

	var jsonResp struct {
		Data struct {
			ChatSessionID string `json:"chat_session_id"`
			MessageID     string `json:"message_id"`
		} `json:"data"`
		Message string `json:"message"`
		Code    int    `json:"code"`
	}

	if err := json.Unmarshal(bodyBytes, &jsonResp); err != nil {
		return "", "", err
	}

	if jsonResp.Code != 0 {
		return "", "", fmt.Errorf("trae create_session code=%d: %s", jsonResp.Code, jsonResp.Message)
	}

	return jsonResp.Data.ChatSessionID, jsonResp.Data.MessageID, nil
}

func (e *TraeExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	messages, _ := m["messages"].([]any)
	query := flattenTraeQuery(messages)

	headers := e.buildHeaders(cred, stream)

	sessionID, messageID, err := e.createSession(ctx, headers, query, model, cred)
	if err != nil {
		return jsonErr(502, err.Error(), "api_error", ""), nil
	}

	eventsURL := fmt.Sprintf("%s/chat_sessions/%s/events?reply_to_message_id=%s",
		e.base(cred), sessionID, url.QueryEscape(messageID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eventsURL, nil)
	if err != nil {
		return nil, err
	}

	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}

	res, err := e.client().Do(req)
	if err != nil {
		return nil, err
	}

	if res == nil || res.Body == nil {
		return nil, fmt.Errorf("nil response from upstream")
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		DrainBody(res.Body)
		_ = res.Body.Close()

		return jsonErr(res.StatusCode, fmt.Sprintf("events stream HTTP %d", res.StatusCode), "api_error", ""), nil
	}

	cid := fmt.Sprintf("chatcmpl-trae-%d", time.Now().UnixMilli())
	created := time.Now().Unix()

	if stream {
		sseBody := wrapTraeEventStream(res.Body, model, cid, created)

		return &Result{
			StatusCode: 200,
			Header: http.Header{
				"Content-Type":  []string{"text/event-stream"},
				"Cache-Control": []string{"no-cache"},
				"Connection":    []string{"keep-alive"},
			},
			Body: sseBody,
		}, nil
	}

	jsonResp, err := collectTraeNonStreaming(res.Body, model, cid, created)
	if err != nil {
		return jsonErr(502, err.Error(), "api_error", ""), nil
	}

	return &Result{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(jsonResp)),
	}, nil
}

type traePlanState struct {
	thoughts map[string]string
	order    []string
	sent     int
}

func (s *traePlanState) renderNewText(data map[string]any) string {
	pid, _ := data["id"].(string)
	if pid == "" {
		return ""
	}

	if s.thoughts == nil {
		s.thoughts = make(map[string]string)
	}

	if _, exists := s.thoughts[pid]; !exists {
		s.order = append(s.order, pid)
	}

	t, _ := data["thought"].(string)
	if len(t) >= len(s.thoughts[pid]) {
		s.thoughts[pid] = t
	}

	var sb strings.Builder
	for _, id := range s.order {
		sb.WriteString(s.thoughts[id])
	}

	full := sb.String()
	piece := full[s.sent:]
	s.sent = len(full)

	return piece
}

func wrapTraeEventStream(r io.ReadCloser, model, cid string, created int64) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer r.Close()
		defer pw.Close()

		writeSSE := func(obj map[string]any) {
			b, _ := json.Marshal(obj)
			_, _ = pw.Write([]byte("data: "))
			_, _ = pw.Write(b)
			_, _ = pw.Write([]byte("\n\n"))
		}

		writeSSE(map[string]any{
			"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil,
			}},
		})

		sc := bufio.NewScanner(r)

		var currentEvent string

		state := &traePlanState{
			thoughts: make(map[string]string),
			order:    nil,
			sent:     0,
		}

		var usage map[string]any

		var errorEvent map[string]any

		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r\n")
			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

				var data map[string]any

				if err := json.Unmarshal([]byte(payload), &data); err != nil {
					data = map[string]any{"_raw": payload}
				}

				if currentEvent == "error" {
					errorEvent = data
					break
				}

				if currentEvent == "token_usage" {
					usage = data
				}

				if currentEvent == "plan_item" {
					piece := state.renderNewText(data)
					if piece != "" {
						writeSSE(map[string]any{
							"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
							"choices": []any{map[string]any{
								"index": 0, "delta": map[string]any{"content": piece}, "finish_reason": nil,
							}},
						})
					}
				}

				if currentEvent == "done" {
					break
				}
			} else if line == "" {
				currentEvent = ""
			}
		}

		if errorEvent != nil {
			writeSSE(map[string]any{
				"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []any{},
				"error": map[string]any{
					"message": fmt.Sprintf("trae %v: %v", errorEvent["code"], errorEvent["message"]),
					"type":    "api_error",
				},
			})
		} else {
			choiceChunk := map[string]any{
				"id": cid, "object": "chat.completion.chunk", "created": created, "model": model,
				"choices": []any{map[string]any{
					"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
				}},
			}
			if usage != nil {
				choiceChunk["usage"] = map[string]any{
					"prompt_tokens":     usage["prompt_tokens"],
					"completion_tokens": usage["completion_tokens"],
					"total_tokens":      usage["total_tokens"],
				}
			}

			writeSSE(choiceChunk)
		}

		_, _ = pw.Write([]byte("data: [DONE]\n\n"))
	}()

	return pr
}

func collectTraeNonStreaming(r io.ReadCloser, model, cid string, created int64) ([]byte, error) {
	defer r.Close()

	sc := bufio.NewScanner(r)

	var currentEvent string

	state := &traePlanState{
		thoughts: make(map[string]string),
		order:    nil,
		sent:     0,
	}

	var usage map[string]any

	var errorEvent map[string]any

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

			var data map[string]any

			if err := json.Unmarshal([]byte(payload), &data); err != nil {
				data = map[string]any{"_raw": payload}
			}

			if currentEvent == "error" {
				errorEvent = data
				break
			}

			if currentEvent == "token_usage" {
				usage = data
			}

			if currentEvent == "plan_item" {
				state.renderNewText(data)
			}

			if currentEvent == "done" {
				break
			}
		} else if line == "" {
			currentEvent = ""
		}
	}

	if errorEvent != nil {
		return nil, fmt.Errorf("trae %v: %v", errorEvent["code"], errorEvent["message"])
	}

	var sb strings.Builder
	for _, id := range state.order {
		sb.WriteString(state.thoughts[id])
	}

	content := sb.String()

	out := map[string]any{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
	}
	if usage != nil {
		out["usage"] = map[string]any{
			"prompt_tokens":     usage["prompt_tokens"],
			"completion_tokens": usage["completion_tokens"],
			"total_tokens":      usage["total_tokens"],
		}
	}

	return json.Marshal(out)
}
