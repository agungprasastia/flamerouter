package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/schema"
	"strings"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatCursor, openaiToCursorRequest, nil)
}

func openaiToCursorRequest(model string, body map[string]any, stream bool, _ map[string]any) map[string]any {
	messages := convertCursorMessages(body)

	result := map[string]any{
		"model":      model,
		"messages":   messages,
		"stream":     stream,
		"max_tokens": 16384,
	}

	if temp, ok := body["temperature"]; ok {
		result["temperature"] = temp
	}

	if tp, ok := body["top_p"]; ok {
		result["top_p"] = tp
	}

	if tools, ok := body["tools"].([]any); ok && len(tools) > 0 {
		result["tools"] = tools
	}

	if tc, ok := body["tool_choice"]; ok {
		result["tool_choice"] = tc
	}

	return result
}

func recordCursorToolCalls(tc []any, meta map[string]string) {
	for _, tcRaw := range tc {
		toolCall, ok := tcRaw.(map[string]any)
		if !ok || toolCall == nil {
			continue
		}

		id, idOk := toolCall["id"].(string)
		fn, fnOk := toolCall["function"].(map[string]any)

		if idOk && fnOk && id != "" && fn != nil {
			if name, nOk := fn["name"].(string); nOk && name != "" {
				meta[id] = name
			}
		}
	}
}

func recordCursorToolUses(arr []any, meta map[string]string) {
	for _, blockRaw := range arr {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		if block["type"] == "tool_use" {
			id, idOk := block["id"].(string)
			name, nameOk := block["name"].(string)

			if idOk && nameOk && id != "" && name != "" {
				meta[id] = name
			}
		}
	}
}

func buildCursorToolCallMeta(messages []any) map[string]string {
	meta := make(map[string]string)

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil || msg["role"] != schema.RoleAssistant {
			continue
		}

		if tc, ok := msg["tool_calls"].([]any); ok {
			recordCursorToolCalls(tc, meta)
		}

		if arr, ok := msg["content"].([]any); ok {
			recordCursorToolUses(arr, meta)
		}
	}

	return meta
}

func extractToolResultText(content any) string {
	if c, ok := content.(string); ok {
		return c
	}

	if cArr, ok := content.([]any); ok {
		var parts []string

		for _, cBlock := range cArr {
			if cm, ok := cBlock.(map[string]any); ok && cm != nil {
				if t, ok := cm["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}

		return strings.Join(parts, "")
	}

	return ""
}

func convertCursorToolResultBlock(block map[string]any, meta map[string]string) string {
	tuid, ok := block["tool_use_id"].(string)
	if !ok {
		tuid = ""
	}

	toolName := meta[tuid]
	if toolName == "" {
		toolName = "tool"
	}

	resultText := extractToolResultText(block["content"])

	return buildCursorToolResultBlock(toolName, tuid, resultText)
}

func convertCursorUserContentBlocks(arr []any, meta map[string]string) (string, bool) {
	var parts []string

	for _, blockRaw := range arr {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		btype, ok := block["type"].(string)
		if !ok {
			continue
		}

		if btype == "text" || btype == schema.ClaudeBlockText {
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		} else if btype == "tool_result" {
			parts = append(parts, convertCursorToolResultBlock(block, meta))
		}
	}

	joined := strings.Join(parts, "\n")
	if joined != "" {
		return joined, true
	}

	return "", false
}

func convertCursorUserMsg(msg map[string]any, meta map[string]string) (map[string]any, bool) {
	if arr, ok := msg["content"].([]any); ok {
		if joined, ok := convertCursorUserContentBlocks(arr, meta); ok {
			return map[string]any{"role": schema.RoleUser, "content": joined}, true
		}
	}

	text := extractCursorText(msg["content"])
	if text != "" {
		return map[string]any{"role": schema.RoleUser, "content": text}, true
	}

	return nil, false
}

func parseCursorToolUseBlock(block map[string]any) (map[string]any, bool) {
	if block["type"] != "tool_use" {
		return nil, false
	}

	id, idOk := block["id"].(string)
	name, nameOk := block["name"].(string)

	if !idOk || !nameOk || id == "" {
		return nil, false
	}

	inputStr := "{}"

	if inputJSON, ok := block["input"].(map[string]any); ok && inputJSON != nil {
		if b, err := json.Marshal(inputJSON); err == nil {
			inputStr = string(b)
		}
	}

	return map[string]any{
		"id":   id,
		"type": schema.OpenaiBlockFunction,
		"function": map[string]any{
			"name":      name,
			"arguments": inputStr,
		},
	}, true
}

func extractAssistantToolUses(arr []any) []any {
	extractedTools := make([]any, 0, len(arr))

	for _, blockRaw := range arr {
		block, ok := blockRaw.(map[string]any)
		if !ok || block == nil {
			continue
		}

		if tool, ok := parseCursorToolUseBlock(block); ok {
			extractedTools = append(extractedTools, tool)
		}
	}

	return extractedTools
}

func convertCursorAssistantMsg(msg map[string]any) (map[string]any, bool) {
	text := extractCursorText(msg["content"])

	if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
		cleanTools := make([]any, 0, len(tc))

		for _, tcRaw := range tc {
			if toolCall, ok := tcRaw.(map[string]any); ok && toolCall != nil {
				cleanTools = append(cleanTools, toolCall)
			}
		}

		return map[string]any{
			"role":       schema.RoleAssistant,
			"content":    text,
			"tool_calls": cleanTools,
		}, true
	}

	if arr, ok := msg["content"].([]any); ok {
		extractedTools := extractAssistantToolUses(arr)
		if len(extractedTools) > 0 {
			return map[string]any{
				"role":       schema.RoleAssistant,
				"content":    text,
				"tool_calls": extractedTools,
			}, true
		}
	}

	if text != "" {
		return map[string]any{
			"role":    schema.RoleAssistant,
			"content": text,
		}, true
	}

	return nil, false
}

func convertSingleCursorMessage(msg map[string]any, meta map[string]string) (map[string]any, bool) {
	role, ok := msg["role"].(string)
	if !ok {
		role = ""
	}

	switch role {
	case schema.RoleSystem:
		text := extractCursorText(msg["content"])

		return map[string]any{
			"role":    schema.RoleUser,
			"content": "[System Instructions]\n" + text,
		}, true
	case schema.RoleTool:
		text := extractCursorText(msg["content"])

		tcid, ok := msg["tool_call_id"].(string)
		if !ok {
			tcid = ""
		}

		toolName := meta[tcid]
		if toolName == "" {
			toolName = "tool"
		}

		return map[string]any{
			"role":    schema.RoleUser,
			"content": buildCursorToolResultBlock(toolName, tcid, text),
		}, true
	case schema.RoleAssistant:
		return convertCursorAssistantMsg(msg)
	case schema.RoleUser:
		return convertCursorUserMsg(msg, meta)
	default:
		text := extractCursorText(msg["content"])
		if text != "" {
			return map[string]any{"role": role, "content": text}, true
		}

		return nil, false
	}
}

func convertCursorMessages(body map[string]any) []any {
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) == 0 {
		return []any{}
	}

	meta := buildCursorToolCallMeta(messages)
	result := make([]any, 0, len(messages))

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		if out, ok := convertSingleCursorMessage(msg, meta); ok {
			result = append(result, out)
		}
	}

	return result
}

func extractCursorText(content any) string {
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
		if !ok || block == nil {
			continue
		}

		if block["type"] == schema.OpenaiBlockText || block["type"] == schema.ClaudeBlockText {
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}

	return strings.Join(parts, "")
}

func buildCursorToolResultBlock(toolName, toolCallID, resultText string) string {
	clean := sanitizeCursorText(resultText)

	var b strings.Builder

	b.WriteString("<tool_result>\n")
	b.WriteString("<tool_name>")
	b.WriteString(xmlEscape(toolName))
	b.WriteString("</tool_name>\n")
	b.WriteString("<tool_call_id>")
	b.WriteString(xmlEscape(toolCallID))
	b.WriteString("</tool_call_id>\n")
	b.WriteString("<result>")
	b.WriteString(xmlEscape(clean))
	b.WriteString("</result>\n")
	b.WriteString("</tool_result>")

	return b.String()
}

func sanitizeCursorText(text string) string {
	var b strings.Builder

	for _, r := range text {
		if (r >= 0x00 && r <= 0x08) || r == 0x0B || r == 0x0C || (r >= 0x0E && r <= 0x1F) || r == 0x7F {
			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")

	return s
}
