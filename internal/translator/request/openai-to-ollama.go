package request

import (
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatOllama, openaiToOllamaRequest, nil)
}

func openaiToOllamaRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	result := map[string]any{
		"model":    model,
		"messages": normalizeOllamaMessages(body),
		"stream":   stream,
	}

	if temp, ok := body["temperature"]; ok {
		if _, ok := result["options"]; !ok {
			result["options"] = map[string]any{}
		}

		result["options"].(map[string]any)["temperature"] = temp
	}

	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 {
		if _, ok := result["options"]; !ok {
			result["options"] = map[string]any{}
		}

		result["options"].(map[string]any)["num_predict"] = int(mt)
	}

	if tp, ok := body["top_p"]; ok {
		if _, ok := result["options"]; !ok {
			result["options"] = map[string]any{}
		}

		result["options"].(map[string]any)["top_p"] = tp
	}

	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		result["tools"] = tools
	}

	if tc, ok := body["tool_choice"]; ok {
		result["tool_choice"] = tc
	}

	return result
}

func normalizeOllamaMessages(body map[string]any) []any {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return []any{}
	}

	toolCallMap := make(map[string]string)

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		if msg["role"] != schema.RoleAssistant {
			continue
		}

		if tc, ok := msg["tool_calls"].([]any); ok {
			for _, tcRaw := range tc {
				toolCall, ok := tcRaw.(map[string]any)
				if !ok {
					continue
				}

				fn, _ := toolCall["function"].(map[string]any)
				name, _ := fn["name"].(string)
				id, _ := toolCall["id"].(string)

				if id != "" && name != "" {
					toolCallMap[id] = name
				}
			}
		}
	}

	var result []any

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)

		if role == schema.RoleTool {
			content := normalizeOllamaContent(msg["content"])
			tcid, _ := msg["tool_call_id"].(string)

			toolName := toolCallMap[tcid]
			if toolName == "" {
				toolName = "unknown_tool"
			}

			result = append(result, map[string]any{
				"role":      schema.RoleTool,
				"tool_name": toolName,
				"content":   content,
			})

			continue
		}

		if role == schema.RoleAssistant {
			content := normalizeOllamaContent(msg["content"])
			out := map[string]any{
				"role":    schema.RoleAssistant,
				"content": content,
			}

			if tc, ok := msg["tool_calls"].([]any); ok {
				var ollamaToolCalls []any

				for _, tcRaw := range tc {
					toolCall, ok := tcRaw.(map[string]any)
					if !ok {
						continue
					}

					fn, _ := toolCall["function"].(map[string]any)
					name, _ := fn["name"].(string)
					argsStr, _ := fn["arguments"].(string)

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
			}

			result = append(result, out)

			continue
		}

		content := normalizeOllamaContent(msg["content"])
		images := extractOllamaImages(msg["content"])

		if content == "" && role != schema.RoleAssistant {
			continue
		}

		out := map[string]any{
			"role":    role,
			"content": content,
		}
		if len(images) > 0 {
			out["images"] = images
		}

		result = append(result, out)
	}

	return result
}

func normalizeOllamaContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}

	if arr, ok := content.([]any); ok {
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
		if !ok {
			continue
		}

		if block["type"] != schema.OpenaiBlockImageUrl {
			continue
		}

		var url string
		if iu, ok := block["image_url"].(map[string]any); ok {
			url, _ = iu["url"].(string)
		}

		if url == "" {
			continue
		}

		mime, data, err := concerns.ParseDataUri(url)
		if err == nil && mime != "" {
			_ = data
			images = append(images, string(data))
		}
	}

	return images
}
