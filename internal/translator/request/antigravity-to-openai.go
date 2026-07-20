package request

import (
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"encoding/json"
)

func init() {
	translator.Register(translator.FormatAntigravity, translator.FormatOpenAI, antigravityToOpenAIRequest, nil)
}

func antigravityToOpenAIRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	req, _ := body["request"].(map[string]any)
	if req == nil {
		req = body
	}

	result := map[string]any{
		"model":   model,
		"messages": []any{},
		"stream":  stream,
	}

	if genConfig, ok := req["generationConfig"].(map[string]any); ok {
		if mot, ok := genConfig["maxOutputTokens"].(float64); ok {
			result["max_tokens"] = mot
		}
		if temp, ok := genConfig["temperature"].(float64); ok {
			result["temperature"] = temp
		}
		if tp, ok := genConfig["topP"].(float64); ok {
			result["top_p"] = tp
		}
		if tk, ok := genConfig["topK"].(float64); ok {
			result["top_k"] = tk
		}
		if tc, ok := genConfig["thinkingConfig"].(map[string]any); ok {
			if tb, ok := tc["thinkingBudget"].(float64); ok {
				effort := concerns.BudgetToEffort(int(tb))
				if effort != "" {
					result["reasoning_effort"] = effort
				}
			}
		}
	}

	if sysInst, ok := req["systemInstruction"]; ok {
		systemText := extractText(sysInst)
		if systemText != "" {
			messages := result["messages"].([]any)
			messages = append(messages, map[string]any{
				"role":    schema.RoleSystem,
				"content": systemText,
			})
			result["messages"] = messages
		}
	}

	if contents, ok := req["contents"].([]any); ok {
		for _, contentRaw := range contents {
			content, ok := contentRaw.(map[string]any)
			if !ok {
				continue
			}
			converted := convertAntigravityContent(content)
			if converted == nil {
				continue
			}
			messages := result["messages"].([]any)
			if arr, ok := converted.([]any); ok {
				messages = append(messages, arr...)
			} else {
				messages = append(messages, converted)
			}
			result["messages"] = messages
		}
	}

	if tools, ok := req["tools"].([]any); ok {
		var resultTools []any
		for _, toolRaw := range tools {
			tool, ok := toolRaw.(map[string]any)
			if !ok {
				continue
			}
			if funcDecls, ok := tool["functionDeclarations"].([]any); ok {
				for _, funcRaw := range funcDecls {
					fn, ok := funcRaw.(map[string]any)
					if !ok {
						continue
					}
					name, _ := fn["name"].(string)
					desc, _ := fn["description"].(string)
					params, _ := fn["parameters"].(map[string]any)
					resultTools = append(resultTools, map[string]any{
						"type": schema.OpenaiBlockFunction,
						"function": map[string]any{
							"name":       name,
							"description": desc,
							"parameters": normalizeSchemaTypes(params),
						},
					})
				}
			}
		}
		if len(resultTools) > 0 {
			result["tools"] = resultTools
		}
	}

	return result
}

func convertAntigravityContent(content map[string]any) any {
	role, _ := content["role"].(string)
	if role == schema.GeminiRoleModel {
		role = schema.RoleAssistant
	} else if role == schema.GeminiRoleUser {
		role = schema.RoleUser
	}

	parts, ok := content["parts"].([]any)
	if !ok {
		return nil
	}

	var textParts []any
	var toolCalls []any
	var toolResults []any
	var reasoningContent string

	for _, partRaw := range parts {
		part, ok := partRaw.(map[string]any)
		if !ok {
			continue
		}

		if thought, ok := part["thought"].(bool); ok && thought {
			if text, ok := part["text"].(string); ok {
				reasoningContent += text
			}
			continue
		}

		if _, ok := part["thoughtSignature"]; ok {
			if text, ok := part["text"].(string); ok && text != "" {
				textParts = append(textParts, map[string]any{
					"type": schema.OpenaiBlockText,
					"text": text,
				})
			}
			continue
		}

		if text, ok := part["text"].(string); ok && text != "" {
			textParts = append(textParts, map[string]any{
				"type": schema.OpenaiBlockText,
				"text": text,
			})
		}

		if inlineData, ok := part["inlineData"].(map[string]any); ok {
			mimeType, _ := inlineData["mimeType"].(string)
			data, _ := inlineData["data"].(string)
			textParts = append(textParts, map[string]any{
				"type": schema.OpenaiBlockImageUrl,
				"image_url": map[string]any{
					"url": concerns.EncodeDataUri(mimeType, []byte(data)),
				},
			})
		}

		if fc, ok := part["functionCall"].(map[string]any); ok {
			name, _ := fc["name"].(string)
			args, _ := fc["args"].(map[string]any)
			argsJSON, _ := json.Marshal(args)
			toolCalls = append(toolCalls, map[string]any{
				"id":   "call_" + name,
				"type": schema.OpenaiBlockFunction,
				"function": map[string]any{
					"name":      name,
					"arguments": string(argsJSON),
				},
			})
		}

		if fr, ok := part["functionResponse"].(map[string]any); ok {
			name, _ := fr["name"].(string)
			response, _ := fr["response"].(map[string]any)
			result, _ := response["result"]
			resultJSON, _ := json.Marshal(result)
			toolResults = append(toolResults, map[string]any{
				"role":         schema.RoleTool,
				"tool_call_id": "call_" + name,
				"content":      string(resultJSON),
			})
		}
	}

	if len(toolResults) > 0 {
		if len(toolCalls) > 0 || len(textParts) > 0 || reasoningContent != "" {
			assistantMsg := map[string]any{"role": schema.RoleAssistant}
			if len(textParts) > 0 {
				assistantMsg["content"] = concerns.CollapseTextParts(textParts)
			}
			if reasoningContent != "" {
				assistantMsg["reasoning_content"] = reasoningContent
			}
			if len(toolCalls) > 0 {
				assistantMsg["tool_calls"] = toolCalls
			}
			var result []any
			result = append(result, toolResults...)
			result = append(result, assistantMsg)
			return result
		}
		return toolResults
	}

	if len(toolCalls) > 0 {
		msg := map[string]any{"role": schema.RoleAssistant}
		if len(textParts) > 0 {
			msg["content"] = concerns.CollapseTextParts(textParts)
		}
		if reasoningContent != "" {
			msg["reasoning_content"] = reasoningContent
		}
		msg["tool_calls"] = toolCalls
		return msg
	}

	if len(textParts) > 0 || reasoningContent != "" {
		msg := map[string]any{"role": role}
		if len(textParts) > 0 {
			msg["content"] = concerns.CollapseTextParts(textParts)
		}
		if reasoningContent != "" {
			msg["reasoning_content"] = reasoningContent
		}
		return msg
	}

	return nil
}

func normalizeSchemaTypes(s map[string]any) map[string]any {
	if s == nil {
		return s
	}
	result := make(map[string]any)
	for k, v := range s {
		result[k] = v
	}
	delete(result, "enumDescriptions")
	if props, ok := result["properties"].(map[string]any); ok {
		normalized := make(map[string]any)
		for k, v := range props {
			if subSchema, ok := v.(map[string]any); ok {
				normalized[k] = normalizeSchemaTypes(subSchema)
			} else {
				normalized[k] = v
			}
		}
		result["properties"] = normalized
	}
	return result
}

func extractText(instruction any) string {
	switch v := instruction.(type) {
	case string:
		return v
	case map[string]any:
		if parts, ok := v["parts"].([]any); ok {
			var text string
			for _, p := range parts {
				if part, ok := p.(map[string]any); ok {
					if t, ok := part["text"].(string); ok {
						text += t
					}
				}
			}
			return text
		}
	}
	return ""
}
