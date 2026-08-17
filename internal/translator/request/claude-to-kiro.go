// Package request implements request translators between different LLM API formats.
package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"strings"
)

func init() {
	translator.Register(translator.FormatClaude, translator.FormatKiro, claudeToKiroRequest, nil)
}

func buildClaudeKiroSystemPrompt(body map[string]any, model string, agentic bool) string {
	thinkingBudget := translator.ResolveKiroThinkingBudget(body, nil, model)

	var systemPromptParts []string

	if thinkingBudget != nil {
		systemPromptParts = append(systemPromptParts, buildThinkingSystemPrefixFromBudget(*thinkingBudget))
	}

	if agentic {
		systemPromptParts = append(systemPromptParts, translator.KiroAgenticSystemPrompt)
	}

	systemInstruction := extractClaudeSystemText(body["system"])
	if systemInstruction != "" {
		systemPromptParts = append(systemPromptParts, systemInstruction)
	}

	return strings.Join(systemPromptParts, "\n\n")
}

func claudeToKiroRequest(model string, body map[string]any, _ bool, credentials map[string]any) map[string]any {
	messages, ok := body["messages"].([]any)
	if !ok {
		messages = nil
	}

	if tools, ok := body["tools"].([]any); !ok || len(tools) == 0 {
		messages = flattenClaudeToolInteractions(messages)
	}

	upstream, agentic := translator.ResolveKiroModel(model)
	history, currentMessage := convertClaudeToKiroMessages(messages, upstream)
	profileArn := extractKiroSharedCredentials(credentials)
	systemPrompt := buildClaudeKiroSystemPrompt(body, model, agentic)

	currentTimeContext := "[Context: Current time is " + concerns.CurrentTimestamp() + "]"
	contentPrefix := systemPrompt

	if contentPrefix != "" {
		contentPrefix += "\n\n" + currentTimeContext
	} else {
		contentPrefix = currentTimeContext
	}

	return assembleKiroPayload(body, upstream, currentMessage, contentPrefix, profileArn, systemPrompt, history)
}

func parseClaudeToolResultBlock(block map[string]any) string {
	switch c := block["content"].(type) {
	case string:
		return c
	case []any:
		var parts []string

		for _, cBlock := range c {
			if cm, ok := cBlock.(map[string]any); ok && cm != nil {
				if t, ok := cm["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}

		return strings.Join(parts, "\n")
	}

	return ""
}

func flattenClaudeAssistantBlock(blockRaw any) string {
	block, ok := blockRaw.(map[string]any)
	if !ok || block == nil {
		return ""
	}

	btype, ok := block["type"].(string)
	if !ok {
		return ""
	}

	if btype == schema.ClaudeBlockText {
		text, ok := block["text"].(string)
		if !ok {
			return ""
		}

		return text
	}

	if btype == schema.ClaudeBlockToolUse {
		name, ok := block["name"].(string)
		if !ok {
			name = ""
		}

		input, err := json.Marshal(block["input"])
		if err != nil {
			input = []byte("{}")
		}

		return "[Tool call: " + name + "(" + string(input) + ")]"
	}

	return ""
}

func flattenClaudeAssistantMsg(msg map[string]any) map[string]any {
	arr, ok := msg["content"].([]any)
	if !ok {
		return msg
	}

	var parts []string

	for _, blockRaw := range arr {
		s := flattenClaudeAssistantBlock(blockRaw)
		if s != "" {
			parts = append(parts, s)
		}
	}

	return map[string]any{
		"role":    schema.RoleAssistant,
		"content": strings.Join(parts, "\n"),
	}
}

func flattenClaudeUserMsg(msg map[string]any, role string) map[string]any {
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

		btype, okType := block["type"].(string)
		if okType && btype == schema.ClaudeBlockToolResult {
			resultText := parseClaudeToolResultBlock(block)
			newContent = append(newContent, map[string]any{
				"type": schema.ClaudeBlockText,
				"text": "[Tool result: " + resultText + "]",
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

func flattenClaudeToolInteractions(messages []any) []any {
	var out []any

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		role, okRole := msg["role"].(string)
		if !okRole {
			role = ""
		}

		switch role {
		case schema.RoleAssistant:
			out = append(out, flattenClaudeAssistantMsg(msg))
		case schema.RoleUser:
			out = append(out, flattenClaudeUserMsg(msg, role))
		default:
			out = append(out, msg)
		}
	}

	return out
}

func extractClaudeUserBlockText(blockRaw any) string {
	block, ok := blockRaw.(map[string]any)
	if !ok || block == nil {
		return ""
	}

	btype, ok := block["type"].(string)
	if !ok || btype != schema.ClaudeBlockText {
		return ""
	}

	text, ok := block["text"].(string)
	if !ok {
		return ""
	}

	return text
}

func (c *kiroBaseConverter) handleClaudeUser(msg map[string]any) {
	typeof := msg["content"]
	if typeof == nil {
		return
	}

	switch ct := typeof.(type) {
	case string:
		c.pendingUserContent = append(c.pendingUserContent, ct)
	case []any:
		for _, blockRaw := range ct {
			t := extractClaudeUserBlockText(blockRaw)
			if t != "" {
				c.pendingUserContent = append(c.pendingUserContent, t)
			}
		}
	}
}

func (c *kiroBaseConverter) handleClaudeAssistant(msg map[string]any) {
	var textContent string

	if arr, ok := msg["content"].([]any); ok {
		for _, blockRaw := range arr {
			block, ok := blockRaw.(map[string]any)
			if !ok || block == nil {
				continue
			}

			if text, ok := block["text"].(string); ok {
				textContent += text
			}
		}
	} else if s, ok := msg["content"].(string); ok {
		textContent = s
	}

	if textContent != "" {
		c.pendingAssistantContent = append(c.pendingAssistantContent, textContent)
	}
}

func (c *kiroBaseConverter) ingestClaudeMsg(msg map[string]any) {
	role, ok := msg["role"].(string)
	if !ok {
		role = schema.RoleUser
	}

	if role != c.currentRole && c.currentRole != "" {
		c.flush()
	}

	c.currentRole = role
	if role == schema.RoleUser {
		c.handleClaudeUser(msg)
	} else if role == schema.RoleAssistant {
		c.handleClaudeAssistant(msg)
	}
}

func convertClaudeToKiroMessages(messages []any, model string) ([]any, string) {
	return runKiroMessageConversion(messages, model, func(conv *kiroBaseConverter, msg map[string]any) {
		conv.ingestClaudeMsg(msg)
	})
}

func extractClaudeSystemText(system any) string {
	if system == nil {
		return ""
	}

	if s, ok := system.(string); ok {
		return s
	}

	if arr, ok := system.([]any); ok {
		var parts []string

		for _, s := range arr {
			switch v := s.(type) {
			case string:
				parts = append(parts, v)
			case map[string]any:
				if text, ok := v["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}

		return strings.Join(parts, "\n")
	}

	return ""
}
