package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

func init() {
	RegisterSpecialized("grok-cli", &GrokCliExecutor{
		Base: Base{
			Provider: "grok-cli",
			BaseURL:  "https://cli-chat-proxy.grok.com/v1/responses",
			Headers: map[string]string{
				"User-Agent":                "grok-shell/0.2.99 (linux; x86_64)",
				"x-grok-client-identifier":  "grok-shell",
				"x-grok-client-version":     "0.2.99",
			},
		},
	})
}

const (
	grokCliClientID  = "grok-shell"
	grokCliVersion   = "0.2.99"
	grokCliUserAgent = "grok-shell/0.2.99 (linux; x86_64)"
)

var (
	serverIDPattern      = regexp.MustCompile(`^(rs|fc|resp|msg)_`)
	grokCliNativeItemID  = regexp.MustCompile(`^(?:rs|msg|fc)_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	hostedToolTypes      = map[string]bool{
		"web_search": true, "x_search": true, "web_search_preview": true,
		"file_search": true, "image_generation": true, "code_interpreter": true,
		"mcp": true, "local_shell": true,
	}
	responsesAPIAllowlist = map[string]bool{
		"model": true, "input": true, "instructions": true, "tools": true,
		"tool_choice": true, "stream": true, "store": true, "reasoning": true,
		"include": true, "temperature": true, "top_p": true, "max_output_tokens": true,
		"parallel_tool_calls": true, "text": true, "metadata": true, "prompt_cache_key": true,
	}
	effortLevels = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true}
)

// GrokCliExecutor — OpenAI Responses API on cli-chat-proxy.grok.com.
type GrokCliExecutor struct{ Base }

func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
}

func normalizeGrokCliEffort(value any) string {
	s, _ := value.(string)
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "max" {
		return "xhigh"
	}
	if effortLevels[s] {
		return s
	}
	return "high"
}

func supportsGrokCliReasoningEffort(model string) bool {
	return strings.HasPrefix(model, "grok-4.5")
}

func resolveEffortFromModel(modelID string) string {
	for _, level := range []string{"low", "medium", "high", "xhigh"} {
		if strings.HasSuffix(modelID, "-"+level) {
			return level
		}
	}
	return ""
}

func countGrokCliUserTurns(input any) int {
	arr, ok := input.([]any)
	if !ok {
		return 1
	}
	n := 0
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		typ, _ := m["type"].(string)
		if role == "user" && (typ == "" || typ == "message") {
			n++
		}
	}
	if n < 1 {
		return 1
	}
	return n
}

func stripStoredItemReferences(body map[string]any) {
	input, ok := body["input"].([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(input))
	for _, item := range input {
		if s, ok := item.(string); ok {
			if serverIDPattern.MatchString(s) {
				continue
			}
			out = append(out, item)
			continue
		}
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if typ, _ := m["type"].(string); typ == "item_reference" {
			continue
		}
		if id, _ := m["id"].(string); id != "" && serverIDPattern.MatchString(id) && !grokCliNativeItemID.MatchString(id) {
			delete(m, "id")
		}
		out = append(out, m)
	}
	body["input"] = out
}

func normalizeGrokCliTools(body map[string]any) {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		delete(body, "tools")
		delete(body, "tool_choice")
		return
	}
	validNames := map[string]bool{}
	hostedTypes := map[string]bool{}
	out := make([]any, 0, len(tools))
	for _, t := range tools {
		tool, ok := t.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := tool["type"].(string)
		if typ != "function" {
			if hostedToolTypes[typ] {
				hostedTypes[typ] = true
				out = append(out, tool)
				continue
			}
			if typ != "" && tool["function"] == nil {
				if name, _ := tool["name"].(string); name == "" {
					continue
				}
			}
		}
		fn, _ := tool["function"].(map[string]any)
		rawName, _ := tool["name"].(string)
		if rawName == "" && fn != nil {
			rawName, _ = fn["name"].(string)
		}
		name := strings.TrimSpace(rawName)
		if name == "" {
			if hostedToolTypes[typ] {
				out = append(out, tool)
			}
			continue
		}
		desc, _ := tool["description"].(string)
		if desc == "" && fn != nil {
			desc, _ = fn["description"].(string)
		}
		var params any = map[string]any{"type": "object", "properties": map[string]any{}}
		if p, ok := tool["parameters"].(map[string]any); ok {
			params = p
		} else if fn != nil {
			if p, ok := fn["parameters"].(map[string]any); ok {
				params = p
			}
		}
		if len(name) > 128 {
			name = name[:128]
		}
		flat := map[string]any{"type": "function", "name": name, "parameters": params}
		if desc != "" {
			flat["description"] = desc
		}
		validNames[name] = true
		out = append(out, flat)
	}
	if len(out) == 0 {
		delete(body, "tools")
		delete(body, "tool_choice")
		return
	}
	body["tools"] = out
	if tc, ok := body["tool_choice"].(map[string]any); ok {
		choiceType, _ := tc["type"].(string)
		if choiceType == "function" || choiceType == "custom" {
			rawName, _ := tc["name"].(string)
			if rawName == "" {
				if fn, ok := tc["function"].(map[string]any); ok {
					rawName, _ = fn["name"].(string)
				}
			}
			name := strings.TrimSpace(rawName)
			if len(name) > 128 {
				name = name[:128]
			}
			if name == "" || !validNames[name] {
				delete(body, "tool_choice")
			} else {
				body["tool_choice"] = map[string]any{"type": "function", "name": name}
			}
		} else if !hostedTypes[choiceType] {
			delete(body, "tool_choice")
		}
	}
}

func messagesToInput(messages []any) []any {
	out := make([]any, 0, len(messages))
	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "" {
			role = "user"
		}
		content := ""
		switch c := msg["content"].(type) {
		case string:
			content = c
		default:
			b, _ := json.Marshal(c)
			content = string(b)
		}
		out = append(out, map[string]any{"type": "message", "role": role, "content": content})
	}
	return out
}

func (e *GrokCliExecutor) transform(model string, body map[string]any) map[string]any {
	// Ensure input
	input, hasInput := body["input"]
	emptyInput := !hasInput
	if arr, ok := input.([]any); ok && len(arr) == 0 {
		emptyInput = true
	}
	if emptyInput {
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			body["input"] = messagesToInput(msgs)
			delete(body, "messages")
		} else {
			body["input"] = []any{map[string]any{"type": "message", "role": "user", "content": "..."}}
		}
	}
	stripStoredItemReferences(body)
	normalizeGrokCliTools(body)

	body["stream"] = true
	body["store"] = false

	modelEffort := resolveEffortFromModel(model)
	resolved := model
	if modelEffort != "" {
		resolved = strings.TrimSuffix(resolved, "-"+modelEffort)
	}
	if bm, _ := body["model"].(string); bm != "" {
		me := resolveEffortFromModel(bm)
		resolved = bm
		if me != "" {
			modelEffort = me
			resolved = strings.TrimSuffix(resolved, "-"+me)
		}
	}
	body["model"] = resolved

	supportsEffort := supportsGrokCliReasoningEffort(resolved)
	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		reasoning = map[string]any{"summary": "concise"}
		if supportsEffort {
			effort := modelEffort
			if re, ok := body["reasoning_effort"]; ok {
				effort = normalizeGrokCliEffort(re)
			} else if effort == "" {
				effort = "high"
			} else {
				effort = normalizeGrokCliEffort(effort)
			}
			reasoning["effort"] = effort
		}
		body["reasoning"] = reasoning
	} else {
		if supportsEffort {
			effortSrc := reasoning["effort"]
			if effortSrc == nil {
				effortSrc = body["reasoning_effort"]
			}
			if effortSrc == nil && modelEffort != "" {
				effortSrc = modelEffort
			}
			reasoning["effort"] = normalizeGrokCliEffort(effortSrc)
		} else {
			delete(reasoning, "effort")
		}
		if _, has := reasoning["summary"]; !has {
			reasoning["summary"] = "concise"
		}
		body["reasoning"] = reasoning
	}
	delete(body, "reasoning_effort")

	if reasoning != nil {
		if effort, _ := reasoning["effort"].(string); effort != "none" {
			include, _ := body["include"].([]any)
			found := false
			for _, v := range include {
				if s, _ := v.(string); s == "reasoning.encrypted_content" {
					found = true
					break
				}
			}
			if !found {
				include = append(include, "reasoning.encrypted_content")
			}
			body["include"] = include
		}
	}

	for _, k := range []string{
		"messages", "max_tokens", "max_completion_tokens", "n", "seed", "logprobs",
		"top_logprobs", "frequency_penalty", "presence_penalty", "logit_bias", "user",
		"stream_options", "prompt_cache_retention", "safety_identifier", "previous_response_id",
	} {
		delete(body, k)
	}
	for k := range body {
		if !responsesAPIAllowlist[k] {
			delete(body, k)
		}
	}
	return body
}

func (e *GrokCliExecutor) buildHeaders(cred Credentials, model string, body map[string]any) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "text/event-stream")
	h.Set("User-Agent", grokCliUserAgent)
	h.Set("x-grok-client-identifier", grokCliClientID)
	h.Set("x-grok-client-version", grokCliVersion)

	sessionID := strPSD(cred, "connectionId")
	if sessionID == "" {
		if pck, _ := body["prompt_cache_key"].(string); pck != "" {
			sessionID = pck
		}
	}
	if sessionID == "" {
		sessionID = randomUUID()
	}
	reqID := randomUUID()
	turnIdx := countGrokCliUserTurns(body["input"])

	h.Set("x-grok-session-id", sessionID)
	h.Set("x-grok-conv-id", sessionID)
	h.Set("x-grok-req-id", reqID)
	h.Set("x-grok-turn-idx", strconv.Itoa(turnIdx))
	if model != "" {
		h.Set("x-grok-model-override", model)
	}

	email := strPSD(cred, "email")
	userID := strPSD(cred, "userId")
	if userID == "" {
		userID = strPSD(cred, "providerUserId")
	}
	if email != "" {
		h.Set("x-email", email)
	}
	if userID != "" {
		h.Set("x-userid", userID)
	}
	if did := strPSD(cred, "deviceId"); did != "" {
		h.Set("x-grok-agent-id", did)
	} else if aid := strPSD(cred, "agentId"); aid != "" {
		h.Set("x-grok-agent-id", aid)
	}

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}
	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}
	return h
}

func (e *GrokCliExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	m = e.transform(model, m)
	resolved, _ := m["model"].(string)
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	url := e.BaseURL
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		url = base
		if !strings.Contains(base, "/responses") {
			url = strings.TrimRight(base, "/") + "/responses"
		}
	}
	return e.DoPOST(ctx, url, e.buildHeaders(cred, resolved, m), payload)
}
