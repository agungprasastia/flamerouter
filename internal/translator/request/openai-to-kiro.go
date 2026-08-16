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

func openaiToKiroRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	messages, _ := body["messages"].([]any)
	tools, _ := body["tools"].([]any)
	temp, _ := body["temperature"].(float64)
	topP, _ := body["top_p"].(float64)

	upstream, agentic := translator.ResolveKiroModel(model)
	thinkingBudget := translator.ResolveKiroThinkingBudget(body, nil, model)

	clientProvidedTools := len(tools) > 0
	if !clientProvidedTools {
		messages = flattenKiroToolInteractions(messages)
	}

	history, currentMessage := convertKiroMessages(messages, tools, upstream, clientProvidedTools)

	rawHeaders := ""
	if credentials != nil {
		rawHeaders, _ = credentials["rawHeaders"].(string)
	}

	authMethod := ""
	profileArn := ""

	if credentials != nil {
		if psd, ok := credentials["providerSpecificData"].(map[string]any); ok {
			authMethod, _ = psd["authMethod"].(string)
			profileArn, _ = psd["profileArn"].(string)
		}
	}

	accountBoundAuth := authMethod == "api_key" || authMethod == "idc" || authMethod == "external_idp"
	if accountBoundAuth {
		if profileArn == "" {
			profileArn = ""
		}
	} else {
		if profileArn == "" {
			profileArn = translator.ResolveDefaultProfileArn(authMethod)
		}
	}

	var systemPromptParts []string
	if thinkingBudget != nil {
		systemPromptParts = append(systemPromptParts, buildThinkingSystemPrefixFromBudget(*thinkingBudget))
	}

	if agentic {
		systemPromptParts = append(systemPromptParts, translator.KiroAgenticSystemPrompt)
	}

	systemPrompt := strings.Join(systemPromptParts, "\n\n")
	currentTimeContext := "[Context: Current time is " + concerns.CurrentTimestamp() + "]"
	contentPrefix := systemPrompt

	if contentPrefix != "" {
		contentPrefix += "\n\n" + currentTimeContext
	} else {
		contentPrefix = currentTimeContext
	}

	_ = rawHeaders

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
		"agentMode": "vibe",
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

	inferenceConfig := map[string]any{}
	inferenceConfig["maxTokens"] = 32000

	if temp != 0 {
		inferenceConfig["temperature"] = temp
	}

	if topP != 0 {
		inferenceConfig["topP"] = topP
	}

	payload["inferenceConfig"] = inferenceConfig

	return payload
}

func flattenKiroToolInteractions(messages []any) []any {
	var out []any

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)

		if role == schema.RoleTool {
			content := extractKiroTextContent(msg["content"])
			out = append(out, map[string]any{
				"role":    schema.RoleUser,
				"content": "[Tool result: " + content + "]",
			})

			continue
		}

		if role == schema.RoleAssistant {
			var parts []string

			if arr, ok := msg["content"].([]any); ok {
				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}

					btype, _ := block["type"].(string)
					if btype == "tool_use" {
						name, _ := block["name"].(string)
						input, _ := json.Marshal(block["input"])
						parts = append(parts, "[Tool call: "+name+"("+string(input)+")]")
					} else if text, ok := block["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			} else if s, ok := msg["content"].(string); ok {
				parts = append(parts, s)
			}

			for _, tcRaw := range msg["tool_calls"].([]any) {
				tc, ok := tcRaw.(map[string]any)
				if !ok {
					continue
				}

				fn, _ := tc["function"].(map[string]any)
				name, _ := fn["name"].(string)
				args, _ := fn["arguments"].(string)
				parts = append(parts, "[Tool call: "+name+"("+args+")]")
			}

			out = append(out, map[string]any{
				"role":    schema.RoleAssistant,
				"content": strings.Join(parts, "\n"),
			})

			continue
		}

		if role == schema.RoleUser {
			if arr, ok := msg["content"].([]any); ok {
				newContent := make([]any, 0, len(arr))

				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}

					if block["type"] == "tool_result" {
						content := extractKiroTextContent(block["content"])
						newContent = append(newContent, map[string]any{
							"type": schema.OpenaiBlockText,
							"text": "[Tool result: " + content + "]",
						})
					} else {
						newContent = append(newContent, block)
					}
				}

				out = append(out, map[string]any{
					"role":    role,
					"content": newContent,
				})

				continue
			}
		}

		out = append(out, msg)
	}

	return out
}

func convertKiroMessages(messages []any, tools []any, model string, clientProvidedTools bool) ([]any, string) {
	var history []any

	var currentMessage string

	var pendingUserContent []string

	var pendingAssistantContent []string

	var currentRole string

	toolsInjected := false

	flushPending := func() {
		if currentRole == schema.RoleUser {
			content := strings.Join(pendingUserContent, "\n\n")
			if content == "" {
				content = "continue"
			}

			history = append(history, map[string]any{
				"userInputMessage": map[string]any{
					"content": content,
					"modelId": model,
				},
			})
			currentMessage = content
			pendingUserContent = nil
		} else if currentRole == schema.RoleAssistant {
			content := strings.Join(pendingAssistantContent, "\n\n")
			if content == "" {
				content = "..."
			}

			history = append(history, map[string]any{
				"assistantResponseMessage": map[string]any{
					"content": content,
				},
			})
			pendingAssistantContent = nil
		}
	}

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)

		wasSystem := role == schema.RoleSystem

		if role == schema.RoleSystem || role == schema.RoleTool {
			role = schema.RoleUser
		}

		if role != currentRole && currentRole != "" {
			flushPending()
		}

		currentRole = role

		if role == schema.RoleUser {
			var content string
			switch c := msg["content"].(type) {
			case string:
				content = c
			case []any:
				var textParts []string

				for _, blockRaw := range c {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}

					if text, ok := block["text"].(string); ok {
						textParts = append(textParts, text)
					}
				}

				content = strings.Join(textParts, "\n")
			}

			if msg["role"] == schema.RoleTool {
				if tcid, ok := msg["tool_call_id"].(string); ok {
					_ = tcid
				}
			} else if content != "" {
				if wasSystem {
					pendingUserContent = append(pendingUserContent, "<instructions>\n"+content+"\n</instructions>")
				} else {
					pendingUserContent = append(pendingUserContent, content)
				}
			}
		} else if role == schema.RoleAssistant {
			var textContent string
			if s, ok := msg["content"].(string); ok {
				textContent = s
			} else if arr, ok := msg["content"].([]any); ok {
				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}

					if text, ok := block["text"].(string); ok {
						textContent += text
					}
				}
			}

			if textContent != "" {
				pendingAssistantContent = append(pendingAssistantContent, textContent)
			}
		}
	}

	if currentRole != "" {
		flushPending()
	}

	for i := len(history) - 1; i >= 0; i-- {
		if item, ok := history[i].(map[string]any); ok {
			if _, ok := item["userInputMessage"]; ok {
				currentMessage = extractKiroUserContent(item)

				history = append(history[:i], history[i+1:]...)

				break
			}
		}
	}

	var mergedHistory []any
	for _, item := range history {
		if len(mergedHistory) > 0 {
			prev, ok := mergedHistory[len(mergedHistory)-1].(map[string]any)
			curr, ok2 := item.(map[string]any)

			if ok && ok2 {
				if _, prevIsUser := prev["userInputMessage"]; prevIsUser {
					if _, currIsUser := curr["userInputMessage"]; currIsUser {
						prevMsg := prev["userInputMessage"].(map[string]any)
						currMsg := curr["userInputMessage"].(map[string]any)
						prevMsg["content"] = prevMsg["content"].(string) + "\n\n" + currMsg["content"].(string)

						continue
					}
				}
			}
		}

		mergedHistory = append(mergedHistory, item)
	}

	_ = tools
	_ = clientProvidedTools
	_ = toolsInjected

	return mergedHistory, currentMessage
}

func extractKiroTextContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string

		for _, blockRaw := range c {
			block, ok := blockRaw.(map[string]any)
			if !ok {
				continue
			}

			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}

		return strings.Join(parts, "\n")
	}

	return ""
}

func extractKiroUserContent(item map[string]any) string {
	uim, ok := item["userInputMessage"].(map[string]any)
	if !ok {
		return ""
	}

	content, _ := uim["content"].(string)

	return content
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

func supportsKiroAdditionalModelRequestFields(model string) bool {
	m := strings.ToLower(strings.ReplaceAll(model, "-", "."))
	if !strings.Contains(m, "claude") {
		return false
	}

	parts := strings.Split(m, ".")
	for _, p := range parts {
		if p == "claude" {
			idx := 0

			for i, pp := range parts {
				if pp == "claude" {
					idx = i
					break
				}
			}

			if idx+1 < len(parts) {
				major := 0

				for _, ch := range parts[idx+1] {
					if ch >= '0' && ch <= '9' {
						major = major*10 + int(ch-'0')
					} else {
						break
					}
				}

				return major >= 4
			}
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
