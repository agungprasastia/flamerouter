package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"strings"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatClaude, openaiToClaudeRequest, nil)
}

func openaiToClaudeRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	result := map[string]any{
		"model":  model,
		"stream": stream,
	}
	ceiling := concerns.DefaultMaxTokens
	if caps := concerns.GetCapabilitiesForModel("", model); caps != nil && caps.MaxOutput > 0 {
		ceiling = caps.MaxOutput
	}
	result["max_tokens"] = concerns.AdjustMaxTokens(body, ceiling)
	if temp, ok := body["temperature"]; ok {
		result["temperature"] = temp
	}
	var systemParts []string
	var messages []any
	toolNameMap := make(map[string]string)
	if bodyMessages, ok := body["messages"].([]any); ok {
		for _, msgRaw := range bodyMessages {
			msg, _ := msgRaw.(map[string]any)
			if msg == nil {
				continue
			}
			role, _ := msg["role"].(string)
			if role == "system" {
				text := extractOpenAITextContent(msg["content"])
				if text != "" {
					systemParts = append(systemParts, text)
				}
				continue
			}
			blocks := getContentBlocksFromOpenAIMessage(msg, toolNameMap)
			messages = append(messages, blocks...)
		}
	}
	if len(messages) > 0 {
		addCacheControlToLastAssistant(messages)
	}
	result["messages"] = messages
	if respFormat, ok := body["response_format"].(map[string]any); ok {
		ftype, _ := respFormat["type"].(string)
		if ftype == "json_schema" {
			if rs, ok := respFormat["json_schema"].(map[string]any); ok {
				if schema, ok := rs["schema"]; ok {
					b, _ := json.MarshalIndent(schema, "", "  ")
					systemParts = append(systemParts, "You must respond with valid JSON that strictly follows this JSON schema:\n```json\n"+string(b)+"\n```\nRespond ONLY with the JSON object, no other text.")
				}
			}
		} else if ftype == "json_object" {
			systemParts = append(systemParts, "You must respond with valid JSON. Respond ONLY with a JSON object, no other text.")
		}
	}
	result["system"] = systemParts
	if tools, ok := body["tools"].([]any); ok {
		var claudeTools []any
		for _, toolRaw := range tools {
			tool, _ := toolRaw.(map[string]any)
			if tool == nil {
				continue
			}
			ttype, _ := tool["type"].(string)
			if ttype != "" && ttype != "function" {
				claudeTools = append(claudeTools, tool)
				continue
			}
			fn, _ := tool["function"].(map[string]any)
			if fn == nil {
				fn = tool
			}
			originalName, _ := fn["name"].(string)
			claudeTools = append(claudeTools, map[string]any{
				"name":         originalName,
				"description":  fn["description"],
				"input_schema": fn["parameters"],
			})
		}
		if len(claudeTools) > 0 {
			result["tools"] = claudeTools
		}
	}
	if tc, ok := body["tool_choice"]; ok {
		result["tool_choice"] = convertOpenAIToolChoice(tc)
	}
	if len(toolNameMap) > 0 {
		result["_toolNameMap"] = toolNameMap
	}
	return result
}

func getContentBlocksFromOpenAIMessage(msg map[string]any, toolNameMap map[string]string) []any {
	role, _ := msg["role"].(string)
	if role == "tool" {
		content, _ := msg["content"].(string)
		tcid, _ := msg["tool_call_id"].(string)
		return []any{map[string]any{
			"type":         "tool_result",
			"tool_use_id":  tcid,
			"content":      content,
		}}
	}
	if role == "user" {
		return getBlocksFromUserMessage(msg, toolNameMap)
	}
	if role == "assistant" {
		return getBlocksFromAssistantMessage(msg, toolNameMap)
	}
	return nil
}

func getBlocksFromUserMessage(msg map[string]any, toolNameMap map[string]string) []any {
	var blocks []any
	content, ok := msg["content"].(string)
	if ok {
		if content != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": content})
		}
		return blocks
	}
	contentArr, ok := msg["content"].([]any)
	if !ok {
		return nil
	}
	for _, partRaw := range contentArr {
		part, _ := partRaw.(map[string]any)
		if part == nil {
			continue
		}
		ptype, _ := part["type"].(string)
		switch ptype {
		case "text":
			if t, ok := part["text"].(string); ok && t != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": t})
			}
		case "tool_result":
			block := map[string]any{
				"type":         "tool_result",
				"tool_use_id":  part["tool_use_id"],
				"content":      part["content"],
			}
			if ie, ok := part["is_error"]; ok {
				block["is_error"] = ie
			}
			blocks = append(blocks, block)
		case "image_url":
			if iu, ok := part["image_url"].(map[string]any); ok {
				if url, ok := iu["url"].(string); ok {
					mimeType, base64Data, err := concerns.ParseDataUri(url)
					if err == nil && mimeType != "" && len(base64Data) > 0 {
						blocks = append(blocks, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type":       "base64",
								"media_type": mimeType,
								"data":       string(base64Data),
							},
						})
					} else if strings.HasPrefix(url, "http") {
						blocks = append(blocks, map[string]any{
							"type": "image",
							"source": map[string]any{
								"type": "url",
								"url":  url,
							},
						})
					}
				}
			}
		}
	}
	return blocks
}

func getBlocksFromAssistantMessage(msg map[string]any, toolNameMap map[string]string) []any {
	var blocks []any
	contentArr, ok := msg["content"].([]any)
	if ok {
		for _, partRaw := range contentArr {
			part, _ := partRaw.(map[string]any)
			if part == nil {
				continue
			}
			ptype, _ := part["type"].(string)
			if ptype == "text" {
				if t, ok := part["text"].(string); ok && t != "" {
					blocks = append(blocks, map[string]any{"type": "text", "text": t})
				}
			} else if ptype == "thinking" {
				blocks = append(blocks, part)
			}
		}
	} else if c, ok := msg["content"].(string); ok && c != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": c})
	}
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
			if args == nil {
				args = make(map[string]any)
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    tc["id"],
				"name":  name,
				"input": args,
			})
		}
	}
	return blocks
}

func addCacheControlToLastAssistant(messages []any) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg, _ := messages[i].(map[string]any)
		if msg == nil {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "assistant" {
			continue
		}
		contentArr, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for j := len(contentArr) - 1; j >= 0; j-- {
			block, _ := contentArr[j].(map[string]any)
			if block == nil {
				continue
			}
			btype, _ := block["type"].(string)
			if btype == "text" || btype == "tool_use" || btype == "tool_result" || btype == "image" {
				block["cache_control"] = map[string]any{"type": "ephemeral"}
				return
			}
		}
		return
	}
}

func convertOpenAIToolChoice(choice any) any {
	if choice == nil {
		return map[string]any{"type": "auto"}
	}
	if s, ok := choice.(string); ok {
		if s == "required" {
			return map[string]any{"type": "any"}
		}
		return map[string]any{"type": "auto"}
	}
	m, ok := choice.(map[string]any)
	if !ok {
		return map[string]any{"type": "auto"}
	}
	if fn, ok := m["function"].(map[string]any); ok {
		if name, ok := fn["name"].(string); ok {
			return map[string]any{"type": "tool", "name": name}
		}
	}
	validTypes := map[string]bool{"auto": true, "any": true, "tool": true, "none": true}
	ttype, _ := m["type"].(string)
	if validTypes[ttype] {
		return m
	}
	return map[string]any{"type": "auto"}
}

func extractOpenAITextContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]any); ok {
		var texts []string
		for _, item := range arr {
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
