package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"strings"
)

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatClaude, openaiToClaudeRequest, nil)
}

func parseResponseFormatSystem(body map[string]any, systemParts []string) []string {
	respFormat, ok := body["response_format"].(map[string]any)
	if !ok || respFormat == nil {
		return systemParts
	}

	ftype, ok := respFormat["type"].(string)
	if !ok {
		return systemParts
	}

	if ftype == "json_object" {
		return append(systemParts, "You must respond with valid JSON. Respond ONLY with a JSON object, no other text.")
	}

	if ftype != "json_schema" {
		return systemParts
	}

	rs, ok := respFormat["json_schema"].(map[string]any)
	if !ok || rs == nil {
		return systemParts
	}

	sc, ok := rs["schema"]
	if !ok {
		return systemParts
	}

	b, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return systemParts
	}

	return append(systemParts, "You must respond with valid JSON that strictly follows this JSON schema:\n```json\n"+string(b)+"\n```\nRespond ONLY with the JSON object, no other text.")
}

func parseOpenAITools(tools []any) []any {
	claudeTools := make([]any, 0, len(tools))

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok || tool == nil {
			continue
		}

		ttype, ok := tool["type"].(string)
		if ok && ttype != "" && ttype != schema.OpenaiBlockFunction {
			claudeTools = append(claudeTools, tool)
			continue
		}

		fn, ok := tool["function"].(map[string]any)
		if !ok || fn == nil {
			fn = tool
		}

		originalName, ok := fn["name"].(string)
		if !ok {
			originalName = ""
		}

		claudeTools = append(claudeTools, map[string]any{
			"name":         originalName,
			"description":  fn["description"],
			"input_schema": fn["parameters"],
		})
	}

	return claudeTools
}

func parseOpenAIMessages(bodyMessages []any, toolNameMap map[string]string) ([]string, []any) {
	var systemParts []string

	messages := make([]any, 0, len(bodyMessages))

	for _, msgRaw := range bodyMessages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok {
			role = ""
		}

		if role == schema.RoleSystem {
			text := extractOpenAITextContent(msg["content"])
			if text != "" {
				systemParts = append(systemParts, text)
			}

			continue
		}

		blocks := getContentBlocksFromOpenAIMessage(msg, toolNameMap)
		if len(blocks) > 0 {
			messages = append(messages, map[string]any{
				"role":    role,
				"content": blocks,
			})
		}
	}

	return systemParts, messages
}

func openaiToClaudeRequest(model string, body map[string]any, stream bool, _ map[string]any) map[string]any {
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

	toolNameMap := make(map[string]string)

	bodyMessages, ok := body["messages"].([]any)
	if !ok {
		bodyMessages = nil
	}

	systemParts, messages := parseOpenAIMessages(bodyMessages, toolNameMap)

	if len(messages) > 0 {
		addCacheControlToLastAssistant(messages)
	}

	result["messages"] = messages
	systemParts = parseResponseFormatSystem(body, systemParts)
	result["system"] = systemParts

	if tools, ok := body["tools"].([]any); ok {
		claudeTools := parseOpenAITools(tools)
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
	role, ok := msg["role"].(string)
	if !ok {
		role = ""
	}

	if role == schema.RoleTool {
		content, ok := msg["content"].(string)
		if !ok {
			content = ""
		}

		tcid, ok := msg["tool_call_id"].(string)
		if !ok {
			tcid = ""
		}

		return []any{map[string]any{
			"type":        schema.ClaudeBlockToolResult,
			"tool_use_id": tcid,
			"content":     content,
		}}
	}

	if role == schema.RoleUser {
		return getBlocksFromUserMessage(msg, toolNameMap)
	}

	if role == schema.RoleAssistant {
		return getBlocksFromAssistantMessage(msg, toolNameMap)
	}

	return nil
}

func parseImageURLBlock(iu map[string]any) map[string]any {
	url, ok := iu["url"].(string)
	if !ok || url == "" {
		return nil
	}

	mimeType, base64Data, err := concerns.ParseDataURI(url)
	if err == nil && mimeType != "" && len(base64Data) > 0 {
		return map[string]any{
			"type": schema.ClaudeBlockImage,
			"source": map[string]any{
				"type":       "base64",
				"media_type": mimeType,
				"data":       string(base64Data),
			},
		}
	}

	if strings.HasPrefix(url, "http") {
		return map[string]any{
			"type": schema.ClaudeBlockImage,
			"source": map[string]any{
				"type": "url",
				"url":  url,
			},
		}
	}

	return nil
}

func parseUserToolResultBlock(part map[string]any) map[string]any {
	block := map[string]any{
		"type":        schema.ClaudeBlockToolResult,
		"tool_use_id": part["tool_use_id"],
		"content":     part["content"],
	}
	if ie, ok := part["is_error"]; ok {
		block["is_error"] = ie
	}

	return block
}

func parseUserPartBlock(part map[string]any) (any, bool) {
	ptype, ok := part["type"].(string)
	if !ok {
		return nil, false
	}

	if ptype == schema.OpenaiBlockText {
		if t, ok := part["text"].(string); ok && t != "" {
			return map[string]any{"type": schema.ClaudeBlockText, "text": t}, true
		}

		return nil, false
	}

	if ptype == schema.ClaudeBlockToolResult {
		return parseUserToolResultBlock(part), true
	}

	if ptype == schema.OpenaiBlockImageURL {
		if iu, ok := part["image_url"].(map[string]any); ok && iu != nil {
			if imgBlock := parseImageURLBlock(iu); imgBlock != nil {
				return imgBlock, true
			}
		}
	}

	return nil, false
}

func getBlocksFromUserMessage(msg map[string]any, _ map[string]string) []any {
	content, ok := msg["content"].(string)
	if ok {
		if content != "" {
			return []any{map[string]any{"type": schema.ClaudeBlockText, "text": content}}
		}

		return nil
	}

	contentArr, ok := msg["content"].([]any)
	if !ok {
		return nil
	}

	blocks := make([]any, 0, len(contentArr))

	for _, partRaw := range contentArr {
		part, ok := partRaw.(map[string]any)
		if !ok || part == nil {
			continue
		}

		if block, ok := parseUserPartBlock(part); ok {
			blocks = append(blocks, block)
		}
	}

	return blocks
}

func parseSingleAssistantToolCall(tcRaw any) (map[string]any, bool) {
	tc, ok := tcRaw.(map[string]any)
	if !ok || tc == nil {
		return nil, false
	}

	fn, ok := tc["function"].(map[string]any)
	if !ok || fn == nil {
		return nil, false
	}

	name, ok := fn["name"].(string)
	if !ok {
		name = ""
	}

	argsStr, ok := fn["arguments"].(string)
	if !ok {
		argsStr = ""
	}

	var args map[string]any
	if argsStr != "" {
		if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
			args = nil
		}
	}

	if args == nil {
		args = make(map[string]any)
	}

	return map[string]any{
		"type":  schema.ClaudeBlockToolUse,
		"id":    tc["id"],
		"name":  name,
		"input": args,
	}, true
}

func extractAssistantToolCalls(msg map[string]any) []any {
	toolCalls, ok := msg["tool_calls"].([]any)
	if !ok {
		return nil
	}

	blocks := make([]any, 0, len(toolCalls))

	for _, tcRaw := range toolCalls {
		if block, ok := parseSingleAssistantToolCall(tcRaw); ok {
			blocks = append(blocks, block)
		}
	}

	return blocks
}

func parseAssistantContentParts(contentArr []any) []any {
	blocks := make([]any, 0, len(contentArr))

	for _, partRaw := range contentArr {
		part, ok := partRaw.(map[string]any)
		if !ok || part == nil {
			continue
		}

		ptype, ok := part["type"].(string)
		if !ok {
			continue
		}

		if ptype == schema.ClaudeBlockText {
			if t, ok := part["text"].(string); ok && t != "" {
				blocks = append(blocks, map[string]any{"type": schema.ClaudeBlockText, "text": t})
			}
		} else if ptype == "thinking" {
			blocks = append(blocks, part)
		}
	}

	return blocks
}

func getBlocksFromAssistantMessage(msg map[string]any, _ map[string]string) []any {
	var blocks []any

	if contentArr, ok := msg["content"].([]any); ok {
		blocks = parseAssistantContentParts(contentArr)
	} else if c, ok := msg["content"].(string); ok && c != "" {
		blocks = append(blocks, map[string]any{"type": schema.ClaudeBlockText, "text": c})
	}

	blocks = append(blocks, extractAssistantToolCalls(msg)...)

	return blocks
}

func markEphemeralBlock(block map[string]any) bool {
	btype, ok := block["type"].(string)
	if !ok {
		return false
	}

	if btype == schema.ClaudeBlockText || btype == schema.ClaudeBlockToolUse || btype == schema.ClaudeBlockToolResult || btype == schema.ClaudeBlockImage {
		block["cache_control"] = map[string]any{"type": "ephemeral"}
		return true
	}

	return false
}

func addCacheControlToBlocks(contentArr []any) bool {
	for j := len(contentArr) - 1; j >= 0; j-- {
		block, ok := contentArr[j].(map[string]any)
		if !ok || block == nil {
			continue
		}

		if markEphemeralBlock(block) {
			return true
		}
	}

	return false
}

func addCacheControlToLastAssistant(messages []any) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok || msg == nil {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok || role != schema.RoleAssistant {
			continue
		}

		contentArr, ok := msg["content"].([]any)
		if !ok {
			continue
		}

		if addCacheControlToBlocks(contentArr) {
			return
		}

		return
	}
}

func convertOpenAIMapToolChoice(m map[string]any) any {
	if fn, ok := m["function"].(map[string]any); ok && fn != nil {
		if name, ok := fn["name"].(string); ok {
			return map[string]any{"type": "tool", "name": name}
		}
	}

	validTypes := map[string]bool{"auto": true, "any": true, "tool": true, "none": true}

	ttype, ok := m["type"].(string)
	if ok && validTypes[ttype] {
		return m
	}

	return map[string]any{"type": "auto"}
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
	if !ok || m == nil {
		return map[string]any{"type": "auto"}
	}

	return convertOpenAIMapToolChoice(m)
}

func extractOpenAITextContent(content any) string {
	if s, ok := content.(string); ok {
		return s
	}

	if arr, ok := content.([]any); ok {
		var texts []string

		for _, item := range arr {
			if m, ok := item.(map[string]any); ok && m != nil {
				if t, ok := m["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}

		return strings.Join(texts, "\n")
	}

	return ""
}
