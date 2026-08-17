package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
)

func init() {
	translator.Register(translator.FormatAntigravity, translator.FormatOpenAI, antigravityToOpenAIRequest, nil)
}

func applyGenConfig(genConfig map[string]any, result map[string]any) {
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

func parseAntigravityTools(tools []any) []any {
	var resultTools []any

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok {
			continue
		}

		funcDecls, ok := tool["functionDeclarations"].([]any)
		if !ok {
			continue
		}

		for _, funcRaw := range funcDecls {
			fn, ok := funcRaw.(map[string]any)
			if !ok || fn == nil {
				continue
			}

			name, ok := fn["name"].(string)
			if !ok {
				name = ""
			}

			desc, ok := fn["description"].(string)
			if !ok {
				desc = ""
			}

			params, ok := fn["parameters"].(map[string]any)
			if !ok {
				params = nil
			}

			resultTools = append(resultTools, map[string]any{
				"type": schema.OpenaiBlockFunction,
				"function": map[string]any{
					"name":        name,
					"description": desc,
					"parameters":  normalizeSchemaTypes(params),
				},
			})
		}
	}

	return resultTools
}

func appendAntigravityContents(contents []any, messages []any) []any {
	for _, contentRaw := range contents {
		content, ok := contentRaw.(map[string]any)
		if !ok {
			continue
		}

		converted := convertAntigravityContent(content)
		if converted == nil {
			continue
		}

		if arr, ok := converted.([]any); ok {
			messages = append(messages, arr...)
		} else {
			messages = append(messages, converted)
		}
	}

	return messages
}

func antigravityToOpenAIRequest(model string, body map[string]any, stream bool, _ map[string]any) map[string]any {
	req, ok := body["request"].(map[string]any)
	if !ok || req == nil {
		req = body
	}

	messages := make([]any, 0)
	result := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}

	if genConfig, ok := req["generationConfig"].(map[string]any); ok {
		applyGenConfig(genConfig, result)
	}

	if sysInst, ok := req["systemInstruction"]; ok {
		systemText := extractText(sysInst)
		if systemText != "" {
			messages = append(messages, map[string]any{
				"role":    schema.RoleSystem,
				"content": systemText,
			})
		}
	}

	if contents, ok := req["contents"].([]any); ok {
		messages = appendAntigravityContents(contents, messages)
	}

	result["messages"] = messages

	if tools, ok := req["tools"].([]any); ok {
		if resultTools := parseAntigravityTools(tools); len(resultTools) > 0 {
			result["tools"] = resultTools
		}
	}

	return result
}

type antigravityPartsCollector struct {
	reasoningContent string
	textParts        []any
	toolCalls        []any
	toolResults      []any
}

func (c *antigravityPartsCollector) processInlineData(inlineData map[string]any) {
	mimeType, ok := inlineData["mimeType"].(string)
	if !ok {
		mimeType = "image/jpeg"
	}

	data, ok := inlineData["data"].(string)
	if !ok {
		data = ""
	}

	c.textParts = append(c.textParts, map[string]any{
		"type": schema.OpenaiBlockImageURL,
		"image_url": map[string]any{
			"url": concerns.EncodeDataURI(mimeType, []byte(data)),
		},
	})
}

func (c *antigravityPartsCollector) processFunctionCall(fc map[string]any) {
	name, ok := fc["name"].(string)
	if !ok {
		name = ""
	}

	args, ok := fc["args"].(map[string]any)
	if !ok {
		args = map[string]any{}
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		argsJSON = []byte("{}")
	}

	c.toolCalls = append(c.toolCalls, map[string]any{
		"id":   "call_" + name,
		"type": schema.OpenaiBlockFunction,
		"function": map[string]any{
			"name":      name,
			"arguments": string(argsJSON),
		},
	})
}

func (c *antigravityPartsCollector) processFunctionResponse(fr map[string]any) {
	name, ok := fr["name"].(string)
	if !ok {
		name = ""
	}

	response, ok := fr["response"].(map[string]any)
	if !ok {
		response = nil
	}

	var result any
	if response != nil {
		result = response["result"]
	}

	resultJSON, err := json.Marshal(result)
	if err != nil {
		resultJSON = []byte("null")
	}

	c.toolResults = append(c.toolResults, map[string]any{
		"role":         schema.RoleTool,
		"tool_call_id": "call_" + name,
		"content":      string(resultJSON),
	})
}

func (c *antigravityPartsCollector) processThought(part map[string]any) bool {
	if thought, ok := part["thought"].(bool); ok && thought {
		if text, ok := part["text"].(string); ok {
			c.reasoningContent += text
		}

		return true
	}

	if _, ok := part["thoughtSignature"]; ok {
		if text, ok := part["text"].(string); ok && text != "" {
			c.textParts = append(c.textParts, map[string]any{"type": schema.OpenaiBlockText, "text": text})
		}

		return true
	}

	return false
}

func (c *antigravityPartsCollector) processPart(part map[string]any) {
	if c.processThought(part) {
		return
	}

	if text, ok := part["text"].(string); ok && text != "" {
		c.textParts = append(c.textParts, map[string]any{"type": schema.OpenaiBlockText, "text": text})
	}

	if inlineData, ok := part["inlineData"].(map[string]any); ok && inlineData != nil {
		c.processInlineData(inlineData)
	}

	if fc, ok := part["functionCall"].(map[string]any); ok && fc != nil {
		c.processFunctionCall(fc)
	}

	if fr, ok := part["functionResponse"].(map[string]any); ok && fr != nil {
		c.processFunctionResponse(fr)
	}
}

func (c *antigravityPartsCollector) buildToolResultAssistant() any {
	if len(c.toolCalls) > 0 || len(c.textParts) > 0 || c.reasoningContent != "" {
		assistantMsg := map[string]any{"role": schema.RoleAssistant}
		if len(c.textParts) > 0 {
			assistantMsg["content"] = concerns.CollapseTextParts(c.textParts)
		}

		if c.reasoningContent != "" {
			assistantMsg["reasoning_content"] = c.reasoningContent
		}

		if len(c.toolCalls) > 0 {
			assistantMsg["tool_calls"] = c.toolCalls
		}

		res := make([]any, 0, len(c.toolResults)+1)
		res = append(res, c.toolResults...)
		res = append(res, assistantMsg)

		return res
	}

	return c.toolResults
}

func (c *antigravityPartsCollector) assembleAssistant(role string) any {
	if len(c.toolResults) > 0 {
		return c.buildToolResultAssistant()
	}

	if len(c.toolCalls) > 0 {
		msg := map[string]any{"role": schema.RoleAssistant, "tool_calls": c.toolCalls}
		if len(c.textParts) > 0 {
			msg["content"] = concerns.CollapseTextParts(c.textParts)
		}

		if c.reasoningContent != "" {
			msg["reasoning_content"] = c.reasoningContent
		}

		return msg
	}

	if len(c.textParts) > 0 || c.reasoningContent != "" {
		msg := map[string]any{"role": role}
		if len(c.textParts) > 0 {
			msg["content"] = concerns.CollapseTextParts(c.textParts)
		}

		if c.reasoningContent != "" {
			msg["reasoning_content"] = c.reasoningContent
		}

		return msg
	}

	return nil
}

func convertAntigravityContent(content map[string]any) any {
	role, ok := content["role"].(string)
	if !ok {
		role = ""
	}

	if role == schema.GeminiRoleModel {
		role = schema.RoleAssistant
	} else if role == schema.GeminiRoleUser {
		role = schema.RoleUser
	}

	parts, ok := content["parts"].([]any)
	if !ok {
		return nil
	}

	var c antigravityPartsCollector

	for _, partRaw := range parts {
		if part, ok := partRaw.(map[string]any); ok {
			c.processPart(part)
		}
	}

	return c.assembleAssistant(role)
}

func normalizeSchemaTypes(s map[string]any) map[string]any {
	if s == nil {
		return nil
	}

	result := make(map[string]any, len(s))
	for k, v := range s {
		result[k] = v
	}

	delete(result, "enumDescriptions")

	if props, ok := result["properties"].(map[string]any); ok {
		normalized := make(map[string]any, len(props))

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
