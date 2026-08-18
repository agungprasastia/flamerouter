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
			Client: nil,
			Headers: map[string]string{
				"User-Agent":               "grok-shell/0.2.99 (linux; x86_64)",
				"x-grok-client-identifier": "grok-shell",
				"x-grok-client-version":    "0.2.99",
			},
			BaseURLs: nil,
			Provider: "grok-cli",
			BaseURL:  "https://cli-chat-proxy.grok.com/v1/responses",
		},
	})
}

const (
	grokCliClientID  = "grok-shell"
	grokCliVersion   = "0.2.99"
	grokCliUserAgent = "grok-shell/0.2.99 (linux; x86_64)"
)

var (
	serverIDPattern     = regexp.MustCompile(`^(rs|fc|resp|msg)_`)
	grokCliNativeItemID = regexp.MustCompile(`^(?:rs|msg|fc)_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	hostedToolTypes     = map[string]bool{
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
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
}

func normalizeGrokCliEffort(value any) string {
	s, ok := value.(string)
	if !ok {
		return "high"
	}

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

		role, _ := m["role"].(string) // nolint:errcheck
		typ, _ := m["type"].(string)  // nolint:errcheck

		if role == "user" && (typ == "" || typ == "message") {
			n++
		}
	}

	if n < 1 {
		return 1
	}

	return n
}

func stripStoredItem(item any) (any, bool) {
	if s, ok := item.(string); ok {
		if serverIDPattern.MatchString(s) {
			return nil, false
		}

		return item, true
	}

	m, ok := item.(map[string]any)
	if !ok {
		return item, true
	}

	if typ, okTyp := m["type"].(string); okTyp && typ == "item_reference" {
		return nil, false
	}

	if id, okID := m["id"].(string); okID && id != "" && serverIDPattern.MatchString(id) && !grokCliNativeItemID.MatchString(id) {
		delete(m, "id")
	}

	return m, true
}

func stripStoredItemReferences(body map[string]any) {
	input, ok := body["input"].([]any)
	if !ok {
		return
	}

	out := make([]any, 0, len(input))

	for _, item := range input {
		if cleaned, keep := stripStoredItem(item); keep {
			out = append(out, cleaned)
		}
	}

	body["input"] = out
}

func extractToolParams(t, fn map[string]any) any {
	if p, ok := t["parameters"].(map[string]any); ok {
		return p
	}

	if fn != nil {
		if p, ok := fn["parameters"].(map[string]any); ok {
			return p
		}
	}

	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func normalizeToolFn(t map[string]any, typ string) (map[string]any, string, bool) {
	fn, _ := t["function"].(map[string]any) // nolint:errcheck
	rawName, _ := t["name"].(string)        // nolint:errcheck

	if rawName == "" && fn != nil {
		rawName, _ = fn["name"].(string) // nolint:errcheck
	}

	name := strings.TrimSpace(rawName)
	if name == "" {
		if hostedToolTypes[typ] {
			return t, "", true
		}

		return nil, "", false
	}

	desc, _ := t["description"].(string) // nolint:errcheck
	if desc == "" && fn != nil {
		desc, _ = fn["description"].(string) // nolint:errcheck
	}

	params := extractToolParams(t, fn)

	if len(name) > 128 {
		name = name[:128]
	}

	flat := map[string]any{"type": "function", "name": name, "parameters": params}
	if desc != "" {
		flat["description"] = desc
	}

	return flat, name, true
}

func resolveToolChoiceName(tc map[string]any) string {
	rawName, _ := tc["name"].(string) // nolint:errcheck
	if rawName == "" {
		if fn, ok := tc["function"].(map[string]any); ok {
			rawName, _ = fn["name"].(string) // nolint:errcheck
		}
	}

	name := strings.TrimSpace(rawName)
	if len(name) > 128 {
		name = name[:128]
	}

	return name
}

func filterGrokCliToolChoice(body map[string]any, validNames map[string]bool, hostedTypes map[string]bool) {
	tc, ok := body["tool_choice"].(map[string]any)
	if !ok {
		return
	}

	choiceType, _ := tc["type"].(string) // nolint:errcheck
	if choiceType == "function" || choiceType == "custom" {
		name := resolveToolChoiceName(tc)
		if name == "" || !validNames[name] {
			delete(body, "tool_choice")
		} else {
			body["tool_choice"] = map[string]any{"type": "function", "name": name}
		}
	} else if !hostedTypes[choiceType] {
		delete(body, "tool_choice")
	}
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

		typ, _ := tool["type"].(string) // nolint:errcheck
		if typ != "function" && hostedToolTypes[typ] {
			hostedTypes[typ] = true

			out = append(out, tool)

			continue
		}

		flat, name, okTool := normalizeToolFn(tool, typ)
		if okTool {
			if name != "" {
				validNames[name] = true
			}

			out = append(out, flat)
		}
	}

	if len(out) == 0 {
		delete(body, "tools")
		delete(body, "tool_choice")

		return
	}

	body["tools"] = out
	filterGrokCliToolChoice(body, validNames, hostedTypes)
}

func messagesToInput(messages []any) []any {
	out := make([]any, 0, len(messages))

	for _, m := range messages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string) // nolint:errcheck
		if role == "" {
			role = "user"
		}

		var content string
		switch c := msg["content"].(type) {
		case string:
			content = c
		default:
			b, err := json.Marshal(c)
			if err == nil {
				content = string(b)
			}
		}

		out = append(out, map[string]any{"type": "message", "role": role, "content": content})
	}

	return out
}

func buildDefaultGrokReasoning(body map[string]any, supportsEffort bool, modelEffort string) map[string]any {
	reasoning := map[string]any{"summary": "concise"}
	if !supportsEffort {
		return reasoning
	}

	effort := modelEffort
	if re, okEffort := body["reasoning_effort"]; okEffort {
		effort = normalizeGrokCliEffort(re)
	} else if effort == "" {
		effort = "high"
	} else {
		effort = normalizeGrokCliEffort(effort)
	}

	reasoning["effort"] = effort

	return reasoning
}

func updateExistingGrokReasoning(reasoning, body map[string]any, supportsEffort bool, modelEffort string) {
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
}

func (e *GrokCliExecutor) applyGrokReasoning(body map[string]any, model, modelEffort string) {
	supportsEffort := supportsGrokCliReasoningEffort(model)

	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		reasoning = buildDefaultGrokReasoning(body, supportsEffort, modelEffort)
	} else {
		updateExistingGrokReasoning(reasoning, body, supportsEffort, modelEffort)
	}

	body["reasoning"] = reasoning
	delete(body, "reasoning_effort")
	includeEncryptedReasoning(body, reasoning)
}

func includeEncryptedReasoning(body map[string]any, reasoning map[string]any) {
	if reasoning == nil {
		return
	}

	effort, _ := reasoning["effort"].(string) // nolint:errcheck
	if effort == "none" {
		return
	}

	include, _ := body["include"].([]any) // nolint:errcheck
	for _, v := range include {
		if s, ok := v.(string); ok && s == "reasoning.encrypted_content" {
			return
		}
	}

	body["include"] = append(include, "reasoning.encrypted_content")
}

func cleanGrokModelName(m string) string {
	m = strings.TrimSpace(m)
	if idx := strings.LastIndex(m, "/"); idx != -1 {
		m = m[idx+1:]
	}
	return m
}

func resolveGrokModelEffort(model string, body map[string]any) (string, string) {
	model = cleanGrokModelName(model)
	modelEffort := resolveEffortFromModel(model)
	resolved := model

	if modelEffort != "" {
		resolved = strings.TrimSuffix(resolved, "-"+modelEffort)
	}

	if bm, ok := body["model"].(string); ok && bm != "" {
		bm = cleanGrokModelName(bm)
		me := resolveEffortFromModel(bm)
		resolved = bm

		if me != "" {
			modelEffort = me
			resolved = strings.TrimSuffix(resolved, "-"+me)
		}
	}

	return resolved, modelEffort
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

	resolved, modelEffort := resolveGrokModelEffort(model, body)

	body["model"] = resolved
	e.applyGrokReasoning(body, resolved, modelEffort)

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

func resolveGrokCliSessionID(cred Credentials, body map[string]any) string {
	sessionID := strPSD(cred, "connectionId")
	if sessionID != "" {
		return sessionID
	}

	if pck, ok := body["prompt_cache_key"].(string); ok && pck != "" {
		return pck
	}

	return randomUUID()
}

func applyGrokCliUserHeaders(h http.Header, cred Credentials) {
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
}

func (e *GrokCliExecutor) buildHeaders(cred Credentials, model string, body map[string]any) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "text/event-stream")
	h.Set("User-Agent", grokCliUserAgent)
	h.Set("x-grok-client-identifier", grokCliClientID)
	h.Set("x-grok-client-version", grokCliVersion)

	sessionID := resolveGrokCliSessionID(cred, body)
	reqID := randomUUID()
	turnIdx := countGrokCliUserTurns(body["input"])

	h.Set("x-grok-session-id", sessionID)
	h.Set("x-grok-conv-id", sessionID)
	h.Set("x-grok-req-id", reqID)
	h.Set("x-grok-turn-idx", strconv.Itoa(turnIdx))

	if model != "" {
		h.Set("x-grok-model-override", model)
	}

	applyGrokCliUserHeaders(h, cred)

	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}

	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}

	return h
}

// Execute performs Grok CLI completion requests.
func (e *GrokCliExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, _ bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	m = e.transform(model, m)

	resolved, ok := m["model"].(string)
	if !ok {
		resolved = model
	}

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
