package request

import (
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatOllama, openaiToOllamaRequest, nil)
}

func ensureOllamaOptions(result map[string]any) map[string]any {
	opts, ok := result["options"].(map[string]any)
	if !ok || opts == nil {
		opts = make(map[string]any)
		result["options"] = opts
	}

	return opts
}

func openaiToOllamaRequest(model string, body map[string]any, stream bool, _ map[string]any) map[string]any {
	result := map[string]any{
		"model":    model,
		"messages": normalizeOllamaMessages(body),
		"stream":   stream,
	}

	if temp, ok := body["temperature"]; ok {
		opts := ensureOllamaOptions(result)
		opts["temperature"] = temp
	}

	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 {
		opts := ensureOllamaOptions(result)
		opts["num_predict"] = int(mt)
	}

	if tp, ok := body["top_p"]; ok {
		opts := ensureOllamaOptions(result)
		opts["top_p"] = tp
	}

	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		result["tools"] = tools
	}

	if tc, ok := body["tool_choice"]; ok {
		result["tool_choice"] = tc
	}

	return result
}

func extractToolCallInfo(tcRaw any) (string, string, bool) {
	toolCall, ok := tcRaw.(map[string]any)
	if !ok {
		return "", "", false
	}

	id, idOk := toolCall["id"].(string)
	if !idOk || id == "" {
		return "", "", false
	}

	fn, fnOk := toolCall["function"].(map[string]any)
	if !fnOk || fn == nil {
		return "", "", false
	}

	name, nOk := fn["name"].(string)
	if !nOk || name == "" {
		return "", "", false
	}

	return id, name, true
}

func buildOllamaToolCallMap(messages []any) map[string]string {
	toolCallMap := make(map[string]string)

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg["role"] != schema.RoleAssistant {
			continue
		}

		tc, ok := msg["tool_calls"].([]any)
		if !ok {
			continue
		}

		for _, tcRaw := range tc {
			if id, name, ok := extractToolCallInfo(tcRaw); ok {
				toolCallMap[id] = name
			}
		}
	}

	return toolCallMap
}

func convertOllamaToolMsg(msg map[string]any, toolCallMap map[string]string) map[string]any {
	content := normalizeOllamaContent(msg["content"])

	tcid, tcidOk := msg["tool_call_id"].(string)
	if !tcidOk {
		tcid = ""
	}

	toolName := toolCallMap[tcid]
	if toolName == "" {
		toolName = "unknown_tool"
	}

	return map[string]any{
		"role":      schema.RoleTool,
		"tool_name": toolName,
		"content":   content,
	}
}

func convertOllamaAssistantMsg(msg map[string]any) map[string]any {
	content := normalizeOllamaContent(msg["content"])
	out := map[string]any{
		"role":    schema.RoleAssistant,
		"content": content,
	}

	tc, ok := msg["tool_calls"].([]any)
	if !ok {
		return out
	}

	ollamaToolCalls := make([]any, 0, len(tc))

	for _, tcRaw := range tc {
		toolCall, ok := tcRaw.(map[string]any)
		if !ok {
			continue
		}

		name := ""
		argsStr := ""

		if fn, fnOk := toolCall["function"].(map[string]any); fnOk && fn != nil {
			if n, nOk := fn["name"].(string); nOk {
				name = n
			}

			if a, aOk := fn["arguments"].(string); aOk {
				argsStr = a
			}
		}

		var args map[string]any
		if argsStr != "" {
			concerns.SafeParseJSON(argsStr, &args)
		}

		ollamaToolCalls = append(ollamaToolCalls, map[string]any{
			"type": schema.OpenaiBlockFunction,
			"function": map[string]any{
				"name":      name,
				"arguments": args,
			},
		})
	}

	out["tool_calls"] = ollamaToolCalls

	return out
}

func convertOllamaDefaultMsg(msg map[string]any, role string) (map[string]any, bool) {
	content := normalizeOllamaContent(msg["content"])
	if content == "" {
		return nil, false
	}

	images := extractOllamaImages(msg["content"])
	out := map[string]any{
		"role":    role,
		"content": content,
	}

	if len(images) > 0 {
		out["images"] = images
	}

	return out, true
}

func normalizeOllamaMessages(body map[string]any) []any {
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return []any{}
	}

	toolCallMap := buildOllamaToolCallMap(messages)
	result := make([]any, 0, len(messages))

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, roleOk := msg["role"].(string)
		if !roleOk {
			role = ""
		}

		switch role {
		case schema.RoleTool:
			result = append(result, convertOllamaToolMsg(msg, toolCallMap))
		case schema.RoleAssistant:
			result = append(result, convertOllamaAssistantMsg(msg))
		default:
			if out, ok := convertOllamaDefaultMsg(msg, role); ok {
				result = append(result, out)
			}
		}
	}

	return result
}

func normalizeOllamaContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}

	arr, ok := content.([]any)
	if !ok {
		return ""
	}

	var parts []string

	for _, blockRaw := range arr {
		block, ok := blockRaw.(map[string]any)
		if !ok {
			continue
		}

		if block["type"] == schema.OpenaiBlockText {
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}

	if len(parts) > 0 {
		result := ""

		for i, p := range parts {
			if i > 0 {
				result += "\n"
			}

			result += p
		}

		return result
	}

	return ""
}

func extractOllamaImages(content any) []string {
	arr, ok := content.([]any)
	if !ok {
		return nil
	}

	var images []string

	for _, blockRaw := range arr {
		block, ok := blockRaw.(map[string]any)
		if !ok || block["type"] != schema.OpenaiBlockImageURL {
			continue
		}

		var url string

		if iu, ok := block["image_url"].(map[string]any); ok {
			if u, uOk := iu["url"].(string); uOk {
				url = u
			}
		}

		if url == "" {
			continue
		}

		mime, data, err := concerns.ParseDataURI(url)
		if err == nil && mime != "" {
			images = append(images, string(data))
		}
	}

	return images
}
