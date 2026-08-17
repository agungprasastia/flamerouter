// Package request implements request translators between different LLM API formats.
package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"strings"

	"github.com/google/uuid"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatKiro, openaiToKiroRequest, nil)
}

func buildKiroInferenceConfig(body map[string]any) map[string]any {
	inferenceConfig := map[string]any{
		"maxTokens": 32000,
	}

	if temp, ok := body["temperature"].(float64); ok && temp != 0 {
		inferenceConfig["temperature"] = temp
	}

	if topP, ok := body["top_p"].(float64); ok && topP != 0 {
		inferenceConfig["topP"] = topP
	}

	return inferenceConfig
}

func assembleKiroPayload(
	body map[string]any,
	upstream, currentMessage, contentPrefix, profileArn, systemPrompt string,
	history []any,
) map[string]any {
	payload := map[string]any{
		"conversationState": map[string]any{
			"chatTriggerType":     "MANUAL",
			"conversationId":      uuid.New().String(),
			"agentContinuationId": uuid.New().String(),
			"agentTaskType":       "vibe",
			"currentMessage": map[string]any{
				"userInputMessage": map[string]any{
					"content": contentPrefix + "\n\n" + currentMessage,
					"modelId": upstream,
					"origin":  "AI_EDITOR",
				},
			},
			"history": history,
		},
		"agentMode":       "vibe",
		"inferenceConfig": buildKiroInferenceConfig(body),
	}

	if profileArn != "" {
		payload["profileArn"] = profileArn
	}

	if systemPrompt != "" {
		payload["systemPrompt"] = systemPrompt
	}

	additionalFields := buildKiroAdditionalModelRequestFields(body, upstream)
	if additionalFields != nil {
		payload["additionalModelRequestFields"] = additionalFields
	}

	return payload
}

func openaiToKiroRequest(model string, body map[string]any, _ bool, credentials map[string]any) map[string]any {
	messages, ok := body["messages"].([]any)
	if !ok {
		messages = nil
	}

	tools, ok := body["tools"].([]any)
	if !ok {
		tools = nil
	}

	upstream, agentic := translator.ResolveKiroModel(model)

	if len(tools) == 0 {
		messages = flattenKiroToolInteractions(messages)
	}

	history, currentMessage := convertKiroMessages(messages, upstream)
	profileArn := extractKiroSharedCredentials(credentials)
	systemPrompt := buildKiroSystemPrompt(body, model, agentic)

	currentTimeContext := "[Context: Current time is " + concerns.CurrentTimestamp() + "]"
	contentPrefix := systemPrompt

	if contentPrefix != "" {
		contentPrefix += "\n\n" + currentTimeContext
	} else {
		contentPrefix = currentTimeContext
	}

	return assembleKiroPayload(body, upstream, currentMessage, contentPrefix, profileArn, systemPrompt, history)
}

func flattenKiroToolUseBlocks(arr []any) []string {
	var parts []string

	for _, blockRaw := range arr {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		btype, ok := block["type"].(string)
		if !ok {
			continue
		}

		if btype == schema.ClaudeBlockToolUse {
			name, okName := block["name"].(string)
			if !okName {
				name = ""
			}

			input, err := json.Marshal(block["input"])
			if err != nil {
				input = []byte("{}")
			}

			parts = append(parts, "[Tool call: "+name+"("+string(input)+")]")
		} else if text, okText := block["text"].(string); okText {
			parts = append(parts, text)
		}
	}

	return parts
}

func flattenSingleOpenAIToolCall(tcRaw any) string {
	tc, ok := tcRaw.(map[string]any)
	if !ok || tc == nil {
		return ""
	}

	fn, ok := tc["function"].(map[string]any)
	if !ok || fn == nil {
		return ""
	}

	name, okName := fn["name"].(string)
	if !okName {
		name = ""
	}

	args, okArgs := fn["arguments"].(string)
	if !okArgs {
		args = ""
	}

	return "[Tool call: " + name + "(" + args + ")]"
}

func flattenKiroAssistantMsg(msg map[string]any) map[string]any {
	var parts []string

	if arr, ok := msg["content"].([]any); ok {
		parts = append(parts, flattenKiroToolUseBlocks(arr)...)
	} else if s, ok := msg["content"].(string); ok {
		parts = append(parts, s)
	}

	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tcRaw := range toolCalls {
			tcStr := flattenSingleOpenAIToolCall(tcRaw)
			if tcStr != "" {
				parts = append(parts, tcStr)
			}
		}
	}

	return map[string]any{
		"role":    schema.RoleAssistant,
		"content": strings.Join(parts, "\n"),
	}
}

func flattenKiroUserMsg(msg map[string]any, role string) map[string]any {
	arr, ok := msg["content"].([]any)
	if !ok {
		return msg
	}

	newContent := make([]any, 0, len(arr))

	for _, blockRaw := range arr {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		bType, ok := block["type"].(string)
		if ok && bType == schema.ClaudeBlockToolResult {
			content := extractKiroTextContent(block["content"])
			newContent = append(newContent, map[string]any{
				"type": schema.OpenaiBlockText,
				"text": "[Tool result: " + content + "]",
			})
		} else {
			newContent = append(newContent, block)
		}
	}

	return map[string]any{
		"role":    role,
		"content": newContent,
	}
}

func flattenKiroToolInteractions(messages []any) []any {
	var out []any

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok {
			role = ""
		}

		switch role {
		case schema.RoleTool:
			content := extractKiroTextContent(msg["content"])
			out = append(out, map[string]any{
				"role":    schema.RoleUser,
				"content": "[Tool result: " + content + "]",
			})
		case schema.RoleAssistant:
			out = append(out, flattenKiroAssistantMsg(msg))
		case schema.RoleUser:
			out = append(out, flattenKiroUserMsg(msg, role))
		default:
			out = append(out, msg)
		}
	}

	return out
}

func extractKiroTextFromBlocks(blocks []any) string {
	var textParts []string

	for _, blockRaw := range blocks {
		if block, ok := blockRaw.(map[string]any); ok && block != nil {
			if text, ok := block["text"].(string); ok {
				textParts = append(textParts, text)
			}
		}
	}

	return strings.Join(textParts, "\n")
}

func (c *kiroBaseConverter) handleOpenAIUser(msg map[string]any, wasSystem bool) {
	var content string

	switch ct := msg["content"].(type) {
	case string:
		content = ct
	case []any:
		content = extractKiroTextFromBlocks(ct)
	}

	if msg["role"] != schema.RoleTool && content != "" {
		if wasSystem {
			c.pendingUserContent = append(c.pendingUserContent, "<instructions>\n"+content+"\n</instructions>")
		} else {
			c.pendingUserContent = append(c.pendingUserContent, content)
		}
	}
}

func (c *kiroBaseConverter) handleOpenAIAssistant(msg map[string]any) {
	var textContent string

	if s, ok := msg["content"].(string); ok {
		textContent = s
	} else if arr, ok := msg["content"].([]any); ok {
		textContent = extractKiroTextFromBlocks(arr)
	}

	if textContent != "" {
		c.pendingAssistantContent = append(c.pendingAssistantContent, textContent)
	}
}

func (c *kiroBaseConverter) ingestOpenAIMsg(msg map[string]any) {
	role, ok := msg["role"].(string)
	if !ok {
		role = schema.RoleUser
	}

	wasSystem := role == schema.RoleSystem

	if role == schema.RoleSystem || role == schema.RoleTool {
		role = schema.RoleUser
	}

	if role != c.currentRole && c.currentRole != "" {
		c.flush()
	}

	c.currentRole = role
	if role == schema.RoleUser {
		c.handleOpenAIUser(msg, wasSystem)
	} else if role == schema.RoleAssistant {
		c.handleOpenAIAssistant(msg)
	}
}

func convertKiroMessages(messages []any, model string) ([]any, string) {
	return runKiroMessageConversion(messages, model, func(conv *kiroBaseConverter, msg map[string]any) {
		conv.ingestOpenAIMsg(msg)
	})
}

func extractKiroTextContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		return extractKiroTextFromBlocks(c)
	}

	return ""
}

func buildKiroAdditionalModelRequestFields(body map[string]any, model string) map[string]any {
	if !supportsKiroAdditionalModelRequestFields(model) {
		return nil
	}

	effort := extractKiroEffortLevel(body)
	if effort == "" {
		return nil
	}

	return map[string]any{
		"thinking": map[string]any{
			"type":    "adaptive",
			"display": "summarized",
		},
		"output_config": map[string]any{
			"effort": effort,
		},
	}
}

func checkClaudeVersionMajor(part string) bool {
	major := 0

	for _, ch := range part {
		if ch >= '0' && ch <= '9' {
			major = major*10 + int(ch-'0')
		} else {
			break
		}
	}

	return major >= 4
}

func supportsKiroAdditionalModelRequestFields(model string) bool {
	m := strings.ToLower(strings.ReplaceAll(model, "-", "."))
	if !strings.Contains(m, "claude") {
		return false
	}

	parts := strings.Split(m, ".")
	for i, p := range parts {
		if p == "claude" && i+1 < len(parts) {
			return checkClaudeVersionMajor(parts[i+1])
		}
	}

	return false
}

func extractKiroEffortLevel(body map[string]any) string {
	if body == nil {
		return ""
	}

	if cfg, ok := body["output_config"].(map[string]any); ok {
		if effort, ok := cfg["effort"].(string); ok {
			return normalizeKiroEffort(effort)
		}
	}

	if re, ok := body["reasoning_effort"].(string); ok {
		return normalizeKiroEffort(re)
	}

	if reasoning, ok := body["reasoning"].(map[string]any); ok {
		if effort, ok := reasoning["effort"].(string); ok {
			return normalizeKiroEffort(effort)
		}
	}

	return ""
}
