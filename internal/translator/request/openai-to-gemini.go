package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/formats"
	"flamerouter/internal/translator/schema"
	"strings"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatGemini, openaiToGeminiRequest, nil)
	translator.Register(translator.FormatOpenAI, translator.FormatGeminiCLI, openaiToGeminiCLIRequest, nil)
	translator.Register(translator.FormatOpenAI, translator.FormatAntigravity, openaiToAntigravityRequest, nil)
}

func ensureGeminiGenConfig(result map[string]any) map[string]any {
	genConfig, ok := result["generationConfig"].(map[string]any)
	if !ok || genConfig == nil {
		genConfig = make(map[string]any)
		result["generationConfig"] = genConfig
	}

	return genConfig
}

func parseSingleGeminiToolCallPart(tcRaw any) (map[string]any, bool) {
	tc, ok := tcRaw.(map[string]any)
	if !ok || tc == nil {
		return nil, false
	}

	fn, fnOk := tc["function"].(map[string]any)
	if !fnOk || fn == nil {
		return nil, false
	}

	name, nameOk := fn["name"].(string)
	if !nameOk {
		name = ""
	}

	argsStr, argsOk := fn["arguments"].(string)
	if !argsOk {
		argsStr = ""
	}

	var args map[string]any
	if argsStr != "" {
		if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
			args = make(map[string]any)
		}
	}

	return map[string]any{
		"functionCall": map[string]any{
			"name": name,
			"args": args,
		},
	}, true
}

func parseGeminiAssistantParts(msg map[string]any, content string) []any {
	parts := []any{map[string]any{"text": content}}

	toolCalls, ok := msg["tool_calls"].([]any)
	if !ok {
		return parts
	}

	for _, tcRaw := range toolCalls {
		if part, ok := parseSingleGeminiToolCallPart(tcRaw); ok {
			parts = append(parts, part)
		}
	}

	return parts
}

func convertGeminiMsg(msg map[string]any, contents []any, systemInstruction *map[string]any) []any {
	role, ok := msg["role"].(string)
	if !ok {
		role = ""
	}

	content := extractOpenAITextContent(msg["content"])

	switch role {
	case schema.RoleSystem:
		if content != "" {
			*systemInstruction = map[string]any{
				"parts": []any{map[string]any{"text": content}},
			}
		}
	case schema.RoleUser:
		contents = append(contents, map[string]any{
			"role":  "user",
			"parts": []any{map[string]any{"text": content}},
		})
	case schema.RoleAssistant:
		contents = append(contents, map[string]any{
			"role":  "model",
			"parts": parseGeminiAssistantParts(msg, content),
		})
	case schema.RoleTool:
		tcid, ok := msg["tool_call_id"].(string)
		if !ok {
			tcid = ""
		}

		name := strings.TrimPrefix(tcid, "call_")
		contents = append(contents, map[string]any{
			"role": "function",
			"parts": []any{map[string]any{
				"functionResponse": map[string]any{
					"name":     name,
					"response": map[string]any{"result": content},
				},
			}},
		})
	}

	return contents
}

func parseGeminiTools(tools []any) []any {
	geminiTools := make([]any, 0, len(tools))

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok || tool == nil {
			continue
		}

		fn, fnOk := tool["function"].(map[string]any)
		if !fnOk || fn == nil {
			continue
		}

		params := fn["parameters"]
		if p, ok := params.(map[string]any); ok && p != nil {
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

	return geminiTools
}

func applyGeminiGenConfig(body map[string]any, result map[string]any) {
	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 {
		genConfig := ensureGeminiGenConfig(result)
		genConfig["maxOutputTokens"] = int(mt)
	}

	if temp, ok := body["temperature"]; ok {
		genConfig := ensureGeminiGenConfig(result)
		genConfig["temperature"] = temp
	}
}

func openaiToGeminiRequest(_ string, body map[string]any, _ bool, _ map[string]any) map[string]any {
	result := map[string]any{
		"contents": []any{},
	}

	applyGeminiGenConfig(body, result)

	contents := make([]any, 0)

	var systemInstruction map[string]any

	if messages, ok := body["messages"].([]any); ok {
		for _, msgRaw := range messages {
			if msg, ok := msgRaw.(map[string]any); ok && msg != nil {
				contents = convertGeminiMsg(msg, contents, &systemInstruction)
			}
		}
	}

	if systemInstruction != nil {
		result["system_instruction"] = systemInstruction
	}

	result["contents"] = contents

	if tools, ok := body["tools"].([]any); ok {
		if geminiTools := parseGeminiTools(tools); len(geminiTools) > 0 {
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

func extractProjectID(credentials map[string]any) string {
	if credentials == nil {
		return formats.GenerateProjectID()
	}

	if psd, ok := credentials["providerSpecificData"].(map[string]any); ok && psd != nil {
		if pid, ok := psd["projectId"].(string); ok && pid != "" {
			return pid
		}
	}

	if pid, ok := credentials["projectId"].(string); ok && pid != "" {
		return pid
	}

	return formats.GenerateProjectID()
}

func openaiToAntigravityRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	inner := openaiToGeminiRequest(model, body, stream, credentials)
	req := map[string]any{
		"contents":         inner["contents"],
		"generationConfig": inner["generationConfig"],
	}

	if si, ok := inner["system_instruction"]; ok && si != nil {
		req["systemInstruction"] = si
	}

	if t, ok := inner["tools"]; ok && t != nil {
		req["tools"] = t
		req["toolConfig"] = map[string]any{
			"functionCallingConfig": map[string]any{"mode": "VALIDATED"},
		}
	}

	return map[string]any{
		"project":     extractProjectID(credentials),
		"model":       model,
		"userAgent":   "antigravity",
		"requestType": "agent",
		"requestId":   formats.GenerateRequestID(),
		"request":     req,
	}
}
