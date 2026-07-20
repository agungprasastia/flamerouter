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
	translator.Register(translator.FormatClaude, translator.FormatKiro, claudeToKiroRequest, nil)
}

func claudeToKiroRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	messages, _ := body["messages"].([]any)
	tools, _ := body["tools"].([]any)
	temp, _ := body["temperature"].(float64)
	topP, _ := body["top_p"].(float64)
	maxTokens := 32000
	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 {
		maxTokens = int(mt)
	}

	clientProvidedTools := len(tools) > 0

	_ = stream

	upstream, agentic := translator.ResolveKiroModel(model)
	thinkingBudget := translator.ResolveKiroThinkingBudget(body, nil, model)

	if !clientProvidedTools {
		messages = flattenClaudeToolInteractions(messages)
	}

	history, currentMessage := convertClaudeToKiroMessages(messages, tools, upstream)

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
		systemPromptParts = append(systemPromptParts, buildThinkingSystemPrefixFromBudgetClaude(*thinkingBudget))
	}
	if agentic {
		systemPromptParts = append(systemPromptParts, translator.KiroAgenticSystemPrompt)
	}
	systemInstruction := extractClaudeSystemText(body["system"])
	if systemInstruction != "" {
		systemPromptParts = append(systemPromptParts, systemInstruction)
	}
	systemPrompt := strings.Join(systemPromptParts, "\n\n")
	currentTimeContext := "[Context: Current time is " + concerns.CurrentTimestamp() + "]"
	contentPrefix := systemPrompt
	if contentPrefix != "" {
		contentPrefix += "\n\n" + currentTimeContext
	} else {
		contentPrefix = currentTimeContext
	}

	payload := map[string]any{
		"conversationState": map[string]any{
			"chatTriggerType":      "MANUAL",
			"conversationId":       uuid.New().String(),
			"agentContinuationId":  uuid.New().String(),
			"agentTaskType":        "vibe",
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

	inferenceConfig := map[string]any{
		"maxTokens": maxTokens,
	}
	if temp != 0 {
		inferenceConfig["temperature"] = temp
	}
	if topP != 0 {
		inferenceConfig["topP"] = topP
	}
	payload["inferenceConfig"] = inferenceConfig

	_ = clientProvidedTools

	return payload
}

func flattenClaudeToolInteractions(messages []any) []any {
	var out []any
	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)

		if role == schema.RoleAssistant {
			if arr, ok := msg["content"].([]any); ok {
				var parts []string
				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}
					btype, _ := block["type"].(string)
					if btype == schema.ClaudeBlockText {
						if text, ok := block["text"].(string); ok {
							parts = append(parts, text)
						}
					} else if btype == "tool_use" {
						name, _ := block["name"].(string)
						input, _ := json.Marshal(block["input"])
						parts = append(parts, "[Tool call: "+name+"("+string(input)+")]")
					}
				}
				out = append(out, map[string]any{
					"role":    schema.RoleAssistant,
					"content": strings.Join(parts, "\n"),
				})
				continue
			}
		}

		if role == schema.RoleUser {
			if arr, ok := msg["content"].([]any); ok {
				newContent := make([]any, 0, len(arr))
				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}
					btype, _ := block["type"].(string)
					if btype == "tool_result" {
						var resultText string
						if c, ok := block["content"].(string); ok {
							resultText = c
						} else if cArr, ok := block["content"].([]any); ok {
							var parts []string
							for _, cBlock := range cArr {
								if cm, ok := cBlock.(map[string]any); ok {
									if t, ok := cm["text"].(string); ok {
										parts = append(parts, t)
									}
								}
							}
							resultText = strings.Join(parts, "\n")
						}
						newContent = append(newContent, map[string]any{
							"type": schema.ClaudeBlockText,
							"text": "[Tool result: " + resultText + "]",
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

func convertClaudeToKiroMessages(messages []any, tools []any, model string) ([]any, string) {
	var history []any
	var currentMessage string
	var pendingUserContent []string
	var pendingAssistantContent []string
	var currentRole string

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

		if role != currentRole && currentRole != "" {
			flushPending()
		}
		currentRole = role

		if role == schema.RoleUser {
			if typeof := msg["content"]; typeof != nil {
				switch c := typeof.(type) {
				case string:
					pendingUserContent = append(pendingUserContent, c)
				case []any:
					for _, blockRaw := range c {
						block, ok := blockRaw.(map[string]any)
						if !ok {
							continue
						}
						btype, _ := block["type"].(string)
						if btype == schema.ClaudeBlockText {
							if text, ok := block["text"].(string); ok {
								pendingUserContent = append(pendingUserContent, text)
							}
						}
					}
				}
			}
		} else if role == schema.RoleAssistant {
			var textContent string
			if arr, ok := msg["content"].([]any); ok {
				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
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
				currentMessage = extractKiroUserContentFromItem(item)
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

	return mergedHistory, currentMessage
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

func extractKiroUserContentFromItem(item map[string]any) string {
	uim, ok := item["userInputMessage"].(map[string]any)
	if !ok {
		return ""
	}
	content, _ := uim["content"].(string)
	return content
}

func buildThinkingSystemPrefixFromBudgetClaude(budget int) string {
	if budget < 1 {
		budget = 1
	}
	if budget > 32000 {
		budget = 32000
	}
	return "<thinking_mode>enabled</thinking_mode>\n<max_thinking_length>" + concerns.MustMarshal(budget) + "</max_thinking_length>"
}
