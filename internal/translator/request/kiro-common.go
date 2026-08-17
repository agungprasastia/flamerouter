// Package request implements request translators between different LLM API formats.
package request

import (
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"strings"
)

type kiroBaseConverter struct {
	currentRole             string
	model                   string
	history                 []any
	pendingUserContent      []string
	pendingAssistantContent []string
}

func (c *kiroBaseConverter) flush() {
	if c.currentRole == schema.RoleUser {
		content := strings.Join(c.pendingUserContent, "\n\n")
		if content == "" {
			content = "continue"
		}

		c.history = append(c.history, map[string]any{
			"userInputMessage": map[string]any{
				"content": content,
				"modelId": c.model,
			},
		})
		c.pendingUserContent = nil
	} else if c.currentRole == schema.RoleAssistant {
		content := strings.Join(c.pendingAssistantContent, "\n\n")
		if content == "" {
			content = "..."
		}

		c.history = append(c.history, map[string]any{
			"assistantResponseMessage": map[string]any{
				"content": content,
			},
		})
		c.pendingAssistantContent = nil
	}
}

func extractKiroSharedCredentials(credentials map[string]any) string {
	if credentials == nil {
		return translator.ResolveDefaultProfileArn("")
	}

	authMethod := ""
	profileArn := ""

	if psd, ok := credentials["providerSpecificData"].(map[string]any); ok && psd != nil {
		if am, ok := psd["authMethod"].(string); ok {
			authMethod = am
		}

		if pa, ok := psd["profileArn"].(string); ok {
			profileArn = pa
		}
	}

	accountBoundAuth := authMethod == "api_key" || authMethod == "idc" || authMethod == "external_idp"
	if !accountBoundAuth && profileArn == "" {
		profileArn = translator.ResolveDefaultProfileArn(authMethod)
	}

	return profileArn
}

func extractOpenAISystemInstruction(messages []any) string {
	var parts []string

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		role, okRole := msg["role"].(string)
		if !okRole || (role != schema.RoleSystem && role != schema.RoleDeveloper) {
			continue
		}

		if text, ok := msg["content"].(string); ok && text != "" {
			parts = append(parts, text)
		}
	}

	return strings.Join(parts, "\n\n")
}

func buildKiroSystemPrompt(body map[string]any, model string, agentic bool) string {
	messages, ok := body["messages"].([]any)
	if !ok {
		messages = nil
	}

	thinkingBudget := translator.ResolveKiroThinkingBudget(body, nil, model)

	var systemPromptParts []string

	if thinkingBudget != nil {
		systemPromptParts = append(systemPromptParts, buildThinkingSystemPrefixFromBudget(*thinkingBudget))
	}

	if agentic {
		systemPromptParts = append(systemPromptParts, translator.KiroAgenticSystemPrompt)
	}

	systemInstruction := extractOpenAISystemInstruction(messages)
	if systemInstruction != "" {
		systemPromptParts = append(systemPromptParts, systemInstruction)
	}

	return strings.Join(systemPromptParts, "\n\n")
}

func extractKiroUserContent(item map[string]any) string {
	uim, ok := item["userInputMessage"].(map[string]any)
	if !ok || uim == nil {
		return ""
	}

	content, ok := uim["content"].(string)
	if !ok {
		return ""
	}

	return content
}

func getKiroUserMessageContent(node any) (map[string]any, string, bool) {
	nodeMap, ok := node.(map[string]any)
	if !ok || nodeMap == nil {
		return nil, "", false
	}

	uim, ok := nodeMap["userInputMessage"].(map[string]any)
	if !ok || uim == nil {
		return nil, "", false
	}

	content, ok := uim["content"].(string)
	if !ok {
		return nil, "", false
	}

	return uim, content, true
}

func tryMergeKiroUserItem(prev, curr any) bool {
	prevUim, pContent, okPrev := getKiroUserMessageContent(prev)
	_, cContent, okCurr := getKiroUserMessageContent(curr)

	if !okPrev || !okCurr {
		return false
	}

	prevUim["content"] = pContent + "\n\n" + cContent

	return true
}

func mergeConsecutiveKiroUserMsgs(history []any) []any {
	mergedHistory := make([]any, 0, len(history))

	for _, item := range history {
		if len(mergedHistory) > 0 && tryMergeKiroUserItem(mergedHistory[len(mergedHistory)-1], item) {
			continue
		}

		mergedHistory = append(mergedHistory, item)
	}

	return mergedHistory
}

func runKiroMessageConversion(
	messages []any,
	model string,
	ingestFn func(conv *kiroBaseConverter, msg map[string]any),
) ([]any, string) {
	conv := &kiroBaseConverter{
		currentRole:             "",
		model:                   model,
		history:                 make([]any, 0, len(messages)),
		pendingUserContent:      nil,
		pendingAssistantContent: nil,
	}

	for _, msgRaw := range messages {
		if msg, ok := msgRaw.(map[string]any); ok && msg != nil {
			ingestFn(conv, msg)
		}
	}

	if conv.currentRole != "" {
		conv.flush()
	}

	history := conv.history

	var currentMessage string

	for i := len(history) - 1; i >= 0; i-- {
		if item, ok := history[i].(map[string]any); ok && item != nil {
			if _, ok := item["userInputMessage"]; ok {
				currentMessage = extractKiroUserContent(item)

				history = append(history[:i], history[i+1:]...)

				break
			}
		}
	}

	mergedHistory := mergeConsecutiveKiroUserMsgs(history)

	return mergedHistory, currentMessage
}

func buildThinkingSystemPrefixFromBudget(budget int) string {
	if budget < 1 {
		budget = 1
	}

	if budget > 32000 {
		budget = 32000
	}

	return "<thinking_mode>enabled</thinking_mode>\n<max_thinking_length>" + concerns.MustMarshal(budget) + "</max_thinking_length>"
}

func normalizeKiroEffort(effort string) string {
	switch strings.ToLower(effort) {
	case "none", "off", "disabled":
		return ""
	case "xhigh", "max":
		return "high"
	case "low", "medium", "high":
		return strings.ToLower(effort)
	}

	return ""
}
