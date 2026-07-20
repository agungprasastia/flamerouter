package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/formats"
	"strings"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatGemini, openaiToGeminiRequest, nil)
	translator.Register(translator.FormatOpenAI, translator.FormatGeminiCLI, openaiToGeminiCLIRequest, nil)
}

func openaiToGeminiRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	result := map[string]any{
		"contents": []any{},
	}
	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 {
		if _, ok := result["generationConfig"]; !ok {
			result["generationConfig"] = map[string]any{}
		}
		result["generationConfig"].(map[string]any)["maxOutputTokens"] = int(mt)
	}
	if temp, ok := body["temperature"]; ok {
		if _, ok := result["generationConfig"]; !ok {
			result["generationConfig"] = map[string]any{}
		}
		result["generationConfig"].(map[string]any)["temperature"] = temp
	}
	var contents []any
	var systemInstruction map[string]any
	if messages, ok := body["messages"].([]any); ok {
		for _, msgRaw := range messages {
			msg, _ := msgRaw.(map[string]any)
			if msg == nil {
				continue
			}
			role, _ := msg["role"].(string)
			content := extractOpenAITextContent(msg["content"])
			switch role {
			case "system":
				if content != "" {
					systemInstruction = map[string]any{
						"parts": []any{map[string]any{"text": content}},
					}
				}
			case "user":
				contents = append(contents, map[string]any{
					"role":  "user",
					"parts": []any{map[string]any{"text": content}},
				})
			case "assistant":
				parts := []any{map[string]any{"text": content}}
				if toolCalls, ok := msg["tool_calls"].([]any); ok {
					for _, tcRaw := range toolCalls {
						tc, _ := tcRaw.(map[string]any)
						if tc == nil {
							continue
						}
						fn, _ := tc["function"].(map[string]any)
						if fn == nil {
							continue
						}
						name, _ := fn["name"].(string)
						argsStr, _ := fn["arguments"].(string)
						var args map[string]any
						if argsStr != "" {
							json.Unmarshal([]byte(argsStr), &args)
						}
						parts = append(parts, map[string]any{
							"functionCall": map[string]any{
								"name": name,
								"args": args,
							},
						})
					}
				}
				contents = append(contents, map[string]any{
					"role":  "model",
					"parts": parts,
				})
			case "tool":
				tcid, _ := msg["tool_call_id"].(string)
				name := strings.TrimPrefix(tcid, "call_")
				contents = append(contents, map[string]any{
					"role":  "function",
					"parts": []any{map[string]any{
						"functionResponse": map[string]any{
							"name":     name,
							"response": map[string]any{"result": content},
						},
					}},
				})
			}
		}
	}
	if systemInstruction != nil {
		result["system_instruction"] = systemInstruction
	}
	result["contents"] = contents
	if tools, ok := body["tools"].([]any); ok {
		var geminiTools []any
		for _, toolRaw := range tools {
			tool, _ := toolRaw.(map[string]any)
			if tool == nil {
				continue
			}
			fn, _ := tool["function"].(map[string]any)
			if fn == nil {
				continue
			}
			params := fn["parameters"]
			if p, ok := params.(map[string]any); ok {
				params = formats.CleanJSONSchemaForAntigravity(p)
			}
			geminiTools = append(geminiTools, map[string]any{
				"function_declarations": []any{map[string]any{
					"name":        fn["name"],
					"description": fn["description"],
					"parameters":  params,
				}},
			})
		}
	if len(geminiTools) > 0 {
			result["tools"] = geminiTools
		}
	}
	if _, ok := result["safetySettings"]; !ok {
		result["safetySettings"] = formats.DefaultSafetySettings
	}
	return result
}

func openaiToGeminiCLIRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	inner := openaiToGeminiRequest(model, body, stream, credentials)
	return map[string]any{
		"contents":           inner["contents"],
		"system_instruction": inner["system_instruction"],
		"tools":              inner["tools"],
		"generationConfig":   inner["generationConfig"],
	}
}
