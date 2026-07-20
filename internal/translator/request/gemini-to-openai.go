package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"strings"
)

func init() {
	translator.Register(translator.FormatGemini, translator.FormatOpenAI, geminiToOpenAIRequest, nil)
	translator.Register(translator.FormatGeminiCLI, translator.FormatOpenAI, geminiToOpenAIRequest, nil)
	translator.Register(translator.FormatVertex, translator.FormatOpenAI, geminiToOpenAIRequest, nil)
}

func geminiToOpenAIRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	result := map[string]any{
		"model":   model,
		"messages": []any{},
		"stream":  stream,
	}
	if gc, ok := body["generationConfig"].(map[string]any); ok {
		if mt, ok := gc["maxOutputTokens"].(float64); ok && mt > 0 {
			result["max_tokens"] = int(mt)
		}
		if temp, ok := gc["temperature"]; ok {
			result["temperature"] = temp
		}
	}
	contents, _ := body["contents"].([]any)
	var systemParts []string
	var messages []any
	for _, cRaw := range contents {
		c, _ := cRaw.(map[string]any)
		if c == nil {
			continue
		}
		role, _ := c["role"].(string)
		parts, _ := c["parts"].([]any)
		if role == "user" {
			for _, pRaw := range parts {
				p, _ := pRaw.(map[string]any)
				if p == nil {
					continue
				}
				if text, ok := p["text"].(string); ok {
					if strings.HasPrefix(text, "<system>") {
						systemParts = append(systemParts, text)
					} else {
						messages = append(messages, map[string]any{"role": "user", "content": text})
					}
				}
				if id, ok := p["inlineData"].(map[string]any); ok {
					mime, _ := id["mimeType"].(string)
					data, _ := id["data"].(string)
					if mime != "" && data != "" {
						messages = append(messages, map[string]any{
							"role": "user",
							"content": []any{map[string]any{
								"type": "image_url",
								"image_url": map[string]any{
									"url": "data:" + mime + ";base64," + data,
								},
							}},
						})
					}
				}
			}
		} else if role == "model" {
			for _, pRaw := range parts {
				p, _ := pRaw.(map[string]any)
				if p == nil {
					continue
				}
				if text, ok := p["text"].(string); ok {
					messages = append(messages, map[string]any{"role": "assistant", "content": text})
				}
				if fc, ok := p["functionCall"].(map[string]any); ok {
					name, _ := fc["name"].(string)
					args, _ := json.Marshal(fc["args"])
					messages = append(messages, map[string]any{
						"role": "assistant",
						"tool_calls": []any{map[string]any{
							"id":   "call_" + name,
							"type": "function",
							"function": map[string]any{
								"name":      name,
								"arguments": string(args),
							},
						}},
					})
				}
			}
		} else if role == "function" || role == "tool" {
			for _, pRaw := range parts {
				p, _ := pRaw.(map[string]any)
				if p == nil {
					continue
				}
				if fr, ok := p["functionResponse"].(map[string]any); ok {
					name, _ := fr["name"].(string)
					resp, _ := json.Marshal(fr["response"])
					messages = append(messages, map[string]any{
						"role":         "tool",
						"tool_call_id": "call_" + name,
						"content":      string(resp),
					})
				}
			}
		}
	}
	if len(systemParts) > 0 {
		messages = append([]any{map[string]any{"role": "system", "content": strings.Join(systemParts, "\n")}}, messages...)
	}
	result["messages"] = messages
	return result
}


