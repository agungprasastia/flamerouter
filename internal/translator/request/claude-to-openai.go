package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"strings"
)

func init() {
	translator.Register(translator.FormatClaude, translator.FormatOpenAI, claudeToOpenAIRequest, nil)
}

func claudeToOpenAIRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	result := map[string]any{
		"model":    model,
		"messages": []any{},
		"stream":   stream,
	}
	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 {
		result["max_tokens"] = int(mt)
	}

	if temp, ok := body["temperature"]; ok {
		result["temperature"] = temp
	}

	var systemParts []string

	if sys, ok := body["system"]; ok {
		switch v := sys.(type) {
		case string:
			systemParts = append(systemParts, v)
		case []any:
			for _, s := range v {
				if sm, ok := s.(map[string]any); ok {
					if txt, ok := sm["text"].(string); ok {
						systemParts = append(systemParts, txt)
					}
				}
			}
		}
	}

	messages := []any{}
	if len(systemParts) > 0 {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": strings.Join(systemParts, "\n\n"),
		})
	}

	if bodyMessages, ok := body["messages"].([]any); ok {
		for _, msgRaw := range bodyMessages {
			msg, _ := msgRaw.(map[string]any)
			if msg == nil {
				continue
			}

			converted := convertClaudeMessage(msg)
			if converted == nil {
				continue
			}

			if arr, ok := converted.([]any); ok {
				messages = append(messages, arr...)
			} else {
				messages = append(messages, converted)
			}
		}
	}

	fixMissingToolResponsesOpenAI(messages)
	result["messages"] = messages

	if tools, ok := body["tools"].([]any); ok {
		var openaiTools []any

		for _, toolRaw := range tools {
			tool, _ := toolRaw.(map[string]any)
			if tool == nil {
				continue
			}

			openaiTools = append(openaiTools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool["name"],
					"description": tool["description"],
					"parameters":  tool["input_schema"],
				},
			})
		}

		if len(openaiTools) > 0 {
			result["tools"] = openaiTools
		}
	}

	if tc, ok := body["tool_choice"]; ok {
		result["tool_choice"] = convertToolChoice(tc)
	}

	if re, ok := body["reasoning_effort"]; ok {
		result["reasoning_effort"] = re
	}

	return result
}

func convertClaudeMessage(msg map[string]any) any {
	role, _ := msg["role"].(string)
	if role == "system" {
		text := extractTextContent(msg["content"])
		if text != "" {
			return map[string]any{"role": "user", "content": "<instructions>\n" + text + "\n</instructions>"}
		}

		return nil
	}

	openaiRole := "user"
	if role == "assistant" {
		openaiRole = "assistant"
	}

	content, ok := msg["content"].(string)
	if ok {
		return map[string]any{"role": openaiRole, "content": content}
	}

	contentArr, ok := msg["content"].([]any)
	if !ok {
		return nil
	}

	var parts []any

	var toolCalls []any

	var toolResults []any

	for _, blockRaw := range contentArr {
		block, _ := blockRaw.(map[string]any)
		if block == nil {
			continue
		}

		btype, _ := block["type"].(string)
		switch btype {
		case "text":
			parts = append(parts, map[string]any{"type": "text", "text": block["text"]})
		case "image":
			if src, ok := block["source"].(map[string]any); ok {
				if src["type"] == "base64" {
					mediaType, _ := src["media_type"].(string)
					data, _ := src["data"].(string)
					parts = append(parts, map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": concerns.EncodeDataUri(mediaType, []byte(data)),
						},
					})
				}
			}
		case "tool_use":
			toolCalls = append(toolCalls, map[string]any{
				"id":   block["id"],
				"type": "function",
				"function": map[string]any{
					"name":      block["name"],
					"arguments": marshalJSON(block["input"]),
				},
			})
		case "tool_result":
			resultContent := ""
			switch c := block["content"].(type) {
			case string:
				resultContent = c
			case []any:
				for _, item := range c {
					if itemMap, ok := item.(map[string]any); ok {
						if itemMap["type"] == "text" {
							if t, ok := itemMap["text"].(string); ok {
								resultContent += t
							}
						}
					}
				}
			}

			toolResults = append(toolResults, map[string]any{
				"role":         "tool",
				"tool_call_id": block["tool_use_id"],
				"content":      resultContent,
			})
		}
	}

	if len(toolResults) > 0 {
		if len(parts) > 0 {
			toolResults = append(toolResults, map[string]any{"role": "user", "content": collapseTextParts(parts)})
		}

		return toolResults
	}

	if len(toolCalls) > 0 {
		result := map[string]any{"role": "assistant"}
		if len(parts) > 0 {
			result["content"] = collapseTextParts(parts)
		}

		result["tool_calls"] = toolCalls

		return result
	}

	if len(parts) > 0 {
		return map[string]any{"role": openaiRole, "content": collapseTextParts(parts)}
	}

	if len(contentArr) == 0 {
		return map[string]any{"role": openaiRole, "content": ""}
	}

	return nil
}

func convertToolChoice(choice any) any {
	if choice == nil {
		return "auto"
	}

	if s, ok := choice.(string); ok {
		return s
	}

	m, ok := choice.(map[string]any)
	if !ok {
		return "auto"
	}

	ttype, _ := m["type"].(string)
	switch ttype {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		if name, ok := m["name"].(string); ok {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}
		}
	}

	return "auto"
}

func fixMissingToolResponsesOpenAI(messages []any) {
	for i := 0; i < len(messages); i++ {
		msg, _ := messages[i].(map[string]any)
		if msg == nil {
			continue
		}

		role, _ := msg["role"].(string)
		if role != "assistant" {
			continue
		}

		toolCalls, ok := msg["tool_calls"].([]any)
		if !ok || len(toolCalls) == 0 {
			continue
		}

		var ids []string

		for _, tcRaw := range toolCalls {
			tc, _ := tcRaw.(map[string]any)
			if tc == nil {
				continue
			}

			if id, ok := tc["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}

		respondedIds := make(map[string]bool)
		insertPos := i + 1

		for j := i + 1; j < len(messages); j++ {
			nextMsg, _ := messages[j].(map[string]any)
			if nextMsg == nil {
				break
			}

			nextRole, _ := nextMsg["role"].(string)
			if nextRole != "tool" {
				break
			}

			if tcid, ok := nextMsg["tool_call_id"].(string); ok && tcid != "" {
				respondedIds[tcid] = true
				insertPos = j + 1
			}
		}

		var missing []any

		for _, id := range ids {
			if !respondedIds[id] {
				missing = append(missing, map[string]any{
					"role":         "tool",
					"tool_call_id": id,
					"content":      "[No response received]",
				})
			}
		}

		if len(missing) > 0 {
			newMsgs := make([]any, 0, len(messages)+len(missing))
			newMsgs = append(newMsgs, messages[:insertPos]...)
			newMsgs = append(newMsgs, missing...)
			newMsgs = append(newMsgs, messages[insertPos:]...)

			for idx := range messages {
				messages[idx] = nil
			}

			for idx, v := range newMsgs {
				if idx < len(messages) {
					messages[idx] = v
				} else {
					messages = append(messages, v)
				}
			}

			i = insertPos + len(missing) - 1
		}
	}
}

func extractTextContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var texts []string

		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}

		return strings.Join(texts, "\n")
	}

	return ""
}

func collapseTextParts(parts []any) string {
	var texts []string

	for _, p := range parts {
		if m, ok := p.(map[string]any); ok {
			if t, ok := m["text"].(string); ok && t != "" {
				texts = append(texts, t)
			}
		}
	}

	return strings.Join(texts, "\n")
}

func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}

	return string(b)
}
