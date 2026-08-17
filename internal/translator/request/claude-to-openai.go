package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"strings"
)

func init() {
	translator.Register(translator.FormatClaude, translator.FormatOpenAI, claudeToOpenAIRequest, nil)
}

func parseClaudeSystem(sys any) []string {
	var systemParts []string

	switch v := sys.(type) {
	case string:
		systemParts = append(systemParts, v)
	case []any:
		for _, s := range v {
			if sm, ok := s.(map[string]any); ok && sm != nil {
				if txt, ok := sm["text"].(string); ok {
					systemParts = append(systemParts, txt)
				}
			}
		}
	}

	return systemParts
}

func parseClaudeTools(tools []any) []any {
	openaiTools := make([]any, 0, len(tools))

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok || tool == nil {
			continue
		}

		openaiTools = append(openaiTools, map[string]any{
			"type": schema.OpenaiBlockFunction,
			"function": map[string]any{
				"name":        tool["name"],
				"description": tool["description"],
				"parameters":  tool["input_schema"],
			},
		})
	}

	return openaiTools
}

func convertClaudeBodyMessages(bodyMessages []any) []any {
	var messages []any

	for _, msgRaw := range bodyMessages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
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

	return messages
}

func parseSystemMessage(sys any) []any {
	systemParts := parseClaudeSystem(sys)
	if len(systemParts) == 0 {
		return nil
	}

	return []any{
		map[string]any{
			"role":    schema.RoleSystem,
			"content": strings.Join(systemParts, "\n\n"),
		},
	}
}

func applyClaudeOptionalParams(body map[string]any, result map[string]any) {
	if mt, ok := body["max_tokens"].(float64); ok && mt > 0 {
		result["max_tokens"] = int(mt)
	}

	if temp, ok := body["temperature"]; ok {
		result["temperature"] = temp
	}

	if tools, ok := body["tools"].([]any); ok {
		openaiTools := parseClaudeTools(tools)
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
}

func claudeToOpenAIRequest(model string, body map[string]any, stream bool, _ map[string]any) map[string]any {
	result := map[string]any{
		"model":    model,
		"messages": []any{},
		"stream":   stream,
	}

	applyClaudeOptionalParams(body, result)

	messages := []any{}

	if sys, ok := body["system"]; ok {
		if sysMsgs := parseSystemMessage(sys); len(sysMsgs) > 0 {
			messages = append(messages, sysMsgs...)
		}
	}

	if bodyMessages, ok := body["messages"].([]any); ok {
		messages = append(messages, convertClaudeBodyMessages(bodyMessages)...)
	}

	messages = fixMissingToolResponsesOpenAI(messages)
	result["messages"] = messages

	return result
}

func parseClaudeToolResultContent(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var sb strings.Builder

		for _, item := range c {
			if itemMap, ok := item.(map[string]any); ok && itemMap != nil {
				if itemMap["type"] == schema.ClaudeBlockText {
					if t, ok := itemMap["text"].(string); ok {
						sb.WriteString(t)
					}
				}
			}
		}

		return sb.String()
	}

	return ""
}

type claudeBlockCollector struct {
	parts       []any
	toolCalls   []any
	toolResults []any
}

func (c *claudeBlockCollector) processImageBlock(block map[string]any) {
	src, ok := block["source"].(map[string]any)
	if !ok || src == nil {
		return
	}

	srcType, ok := src["type"].(string)
	if !ok || srcType != "base64" {
		return
	}

	mediaType, ok := src["media_type"].(string)
	if !ok {
		mediaType = "image/jpeg"
	}

	data, ok := src["data"].(string)
	if !ok {
		data = ""
	}

	c.parts = append(c.parts, map[string]any{
		"type": schema.OpenaiBlockImageURL,
		"image_url": map[string]any{
			"url": concerns.EncodeDataURI(mediaType, []byte(data)),
		},
	})
}

func (c *claudeBlockCollector) processBlock(block map[string]any) {
	btype, ok := block["type"].(string)
	if !ok {
		return
	}

	switch btype {
	case schema.ClaudeBlockText:
		c.parts = append(c.parts, map[string]any{"type": schema.ClaudeBlockText, "text": block["text"]})
	case schema.ClaudeBlockImage:
		c.processImageBlock(block)
	case schema.ClaudeBlockToolUse:
		c.toolCalls = append(c.toolCalls, map[string]any{
			"id":   block["id"],
			"type": schema.OpenaiBlockFunction,
			"function": map[string]any{
				"name":      block["name"],
				"arguments": marshalJSON(block["input"]),
			},
		})
	case schema.ClaudeBlockToolResult:
		c.toolResults = append(c.toolResults, map[string]any{
			"role":         schema.RoleTool,
			"tool_call_id": block["tool_use_id"],
			"content":      parseClaudeToolResultContent(block["content"]),
		})
	}
}

func (c *claudeBlockCollector) assemble(openaiRole string, hasBlocks bool) any {
	if len(c.toolResults) > 0 {
		if len(c.parts) > 0 {
			c.toolResults = append(c.toolResults, map[string]any{"role": schema.RoleUser, "content": collapseTextParts(c.parts)})
		}

		return c.toolResults
	}

	if len(c.toolCalls) > 0 {
		res := map[string]any{"role": schema.RoleAssistant}
		if len(c.parts) > 0 {
			res["content"] = collapseTextParts(c.parts)
		}

		res["tool_calls"] = c.toolCalls

		return res
	}

	if len(c.parts) > 0 {
		return map[string]any{"role": openaiRole, "content": collapseTextParts(c.parts)}
	}

	if !hasBlocks {
		return map[string]any{"role": openaiRole, "content": ""}
	}

	return nil
}

func convertClaudeMessage(msg map[string]any) any {
	role, ok := msg["role"].(string)
	if !ok {
		role = ""
	}

	if role == schema.RoleSystem {
		text := extractTextContent(msg["content"])
		if text != "" {
			return map[string]any{"role": schema.RoleUser, "content": "<instructions>\n" + text + "\n</instructions>"}
		}

		return nil
	}

	openaiRole := schema.RoleUser
	if role == schema.RoleAssistant {
		openaiRole = schema.RoleAssistant
	}

	content, ok := msg["content"].(string)
	if ok {
		return map[string]any{"role": openaiRole, "content": content}
	}

	contentArr, ok := msg["content"].([]any)
	if !ok {
		return nil
	}

	col := &claudeBlockCollector{
		parts:       nil,
		toolCalls:   nil,
		toolResults: nil,
	}

	for _, blockRaw := range contentArr {
		block, ok := blockRaw.(map[string]any)
		if ok && block != nil {
			col.processBlock(block)
		}
	}

	return col.assemble(openaiRole, len(contentArr) > 0)
}

func convertToolChoice(choice any) any {
	if choice == nil {
		return "auto"
	}

	if s, ok := choice.(string); ok {
		return s
	}

	m, ok := choice.(map[string]any)
	if !ok || m == nil {
		return "auto"
	}

	ttype, ok := m["type"].(string)
	if !ok {
		return "auto"
	}

	switch ttype {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "tool":
		if name, ok := m["name"].(string); ok {
			return map[string]any{"type": schema.OpenaiBlockFunction, "function": map[string]any{"name": name}}
		}
	}

	return "auto"
}

func findRespondedIDs(messages []any, startIdx int) (map[string]bool, int) {
	respondedIds := make(map[string]bool)
	insertPos := startIdx

	for j := startIdx; j < len(messages); j++ {
		nextMsg, ok := messages[j].(map[string]any)
		if !ok || nextMsg == nil {
			break
		}

		nextRole, ok := nextMsg["role"].(string)
		if !ok || nextRole != schema.RoleTool {
			break
		}

		if tcid, ok := nextMsg["tool_call_id"].(string); ok && tcid != "" {
			respondedIds[tcid] = true
			insertPos = j + 1
		}
	}

	return respondedIds, insertPos
}

func collectToolCallIDs(toolCalls []any) []string {
	var ids []string

	for _, tcRaw := range toolCalls {
		tc, ok := tcRaw.(map[string]any)
		if ok && tc != nil {
			if id, ok := tc["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}

	return ids
}

func buildMissingToolResults(ids []string, respondedIds map[string]bool) []any {
	var missing []any

	for _, id := range ids {
		if !respondedIds[id] {
			missing = append(missing, map[string]any{
				"role":         schema.RoleTool,
				"tool_call_id": id,
				"content":      "[No response received]",
			})
		}
	}

	return missing
}

func fixMissingToolResponsesOpenAI(messages []any) []any {
	result := append([]any{}, messages...)

	for i := 0; i < len(result); i++ {
		msg, ok := result[i].(map[string]any)
		if !ok || msg == nil {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok || role != schema.RoleAssistant {
			continue
		}

		toolCalls, ok := msg["tool_calls"].([]any)
		if !ok || len(toolCalls) == 0 {
			continue
		}

		ids := collectToolCallIDs(toolCalls)
		respondedIds, insertPos := findRespondedIDs(result, i+1)
		missing := buildMissingToolResults(ids, respondedIds)

		if len(missing) > 0 {
			newMsgs := make([]any, 0, len(result)+len(missing))
			newMsgs = append(newMsgs, result[:insertPos]...)
			newMsgs = append(newMsgs, missing...)
			newMsgs = append(newMsgs, result[insertPos:]...)
			result = newMsgs
			i = insertPos + len(missing) - 1
		}
	}

	return result
}

func extractTextContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var texts []string

		for _, item := range v {
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

func collapseTextParts(parts []any) string {
	var texts []string

	for _, p := range parts {
		if m, ok := p.(map[string]any); ok && m != nil {
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
