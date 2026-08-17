package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/schema"
	"strings"
)

func init() {
	translator.Register(translator.FormatGemini, translator.FormatOpenAI, geminiToOpenAIRequest, nil)
	translator.Register(translator.FormatGeminiCLI, translator.FormatOpenAI, geminiToOpenAIRequest, nil)
	translator.Register(translator.FormatVertex, translator.FormatOpenAI, geminiToOpenAIRequest, nil)
}

func parseGeminiUserPart(p map[string]any, systemParts *[]string, messages []any) []any {
	if text, ok := p["text"].(string); ok {
		if strings.HasPrefix(text, "<system>") {
			*systemParts = append(*systemParts, text)
		} else {
			messages = append(messages, map[string]any{"role": schema.RoleUser, "content": text})
		}
	}

	if id, ok := p["inlineData"].(map[string]any); ok && id != nil {
		mime, mimeOk := id["mimeType"].(string)
		data, dataOk := id["data"].(string)

		if mimeOk && dataOk && mime != "" && data != "" {
			messages = append(messages, map[string]any{
				"role": schema.RoleUser,
				"content": []any{map[string]any{
					"type": schema.OpenaiBlockImageURL,
					"image_url": map[string]any{
						"url": "data:" + mime + ";base64," + data,
					},
				}},
			})
		}
	}

	return messages
}

func parseGeminiModelPart(p map[string]any, messages []any) []any {
	if text, ok := p["text"].(string); ok {
		messages = append(messages, map[string]any{"role": schema.RoleAssistant, "content": text})
	}

	if fc, ok := p["functionCall"].(map[string]any); ok && fc != nil {
		name, nameOk := fc["name"].(string)
		if !nameOk {
			name = ""
		}

		args, err := json.Marshal(fc["args"])
		if err != nil {
			args = []byte("{}")
		}

		messages = append(messages, map[string]any{
			"role": schema.RoleAssistant,
			"tool_calls": []any{map[string]any{
				"id":   "call_" + name,
				"type": schema.OpenaiBlockFunction,
				"function": map[string]any{
					"name":      name,
					"arguments": string(args),
				},
			}},
		})
	}

	return messages
}

func parseGeminiFunctionPart(p map[string]any, messages []any) []any {
	if fr, ok := p["functionResponse"].(map[string]any); ok && fr != nil {
		name, nameOk := fr["name"].(string)
		if !nameOk {
			name = ""
		}

		resp, err := json.Marshal(fr["response"])
		if err != nil {
			resp = []byte("{}")
		}

		messages = append(messages, map[string]any{
			"role":         schema.RoleTool,
			"tool_call_id": "call_" + name,
			"content":      string(resp),
		})
	}

	return messages
}

func parseGeminiContentParts(role string, parts []any, systemParts *[]string, messages []any) []any {
	for _, pRaw := range parts {
		p, ok := pRaw.(map[string]any)
		if !ok || p == nil {
			continue
		}

		switch role {
		case schema.GeminiRoleUser:
			messages = parseGeminiUserPart(p, systemParts, messages)
		case schema.GeminiRoleModel, schema.RoleAssistant:
			messages = parseGeminiModelPart(p, messages)
		case "function", schema.RoleTool:
			messages = parseGeminiFunctionPart(p, messages)
		}
	}

	return messages
}

func convertGeminiContents(contents []any) ([]any, []string) {
	var systemParts []string

	var messages []any

	for _, cRaw := range contents {
		c, ok := cRaw.(map[string]any)
		if !ok || c == nil {
			continue
		}

		role, ok := c["role"].(string)
		if !ok {
			role = ""
		}

		parts, ok := c["parts"].([]any)
		if !ok {
			continue
		}

		messages = parseGeminiContentParts(role, parts, &systemParts, messages)
	}

	return messages, systemParts
}

func geminiToOpenAIRequest(model string, body map[string]any, stream bool, _ map[string]any) map[string]any {
	result := map[string]any{
		"model":    model,
		"messages": []any{},
		"stream":   stream,
	}

	if gc, ok := body["generationConfig"].(map[string]any); ok && gc != nil {
		if mt, ok := gc["maxOutputTokens"].(float64); ok && mt > 0 {
			result["max_tokens"] = int(mt)
		}

		if temp, ok := gc["temperature"]; ok {
			result["temperature"] = temp
		}
	}

	contents, ok := body["contents"].([]any)
	if !ok {
		contents = nil
	}

	messages, systemParts := convertGeminiContents(contents)
	if len(systemParts) > 0 {
		messages = append([]any{map[string]any{"role": schema.RoleSystem, "content": strings.Join(systemParts, "\n")}}, messages...)
	}

	result["messages"] = messages

	return result
}
