// Package request implements request translators between different LLM API formats.
package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/schema"
)

const maxCallIDLen = 64

func clampCallID(id string) string {
	if len(id) > maxCallIDLen {
		return id[:maxCallIDLen]
	}

	return id
}

func init() {
	translator.Register(translator.FormatOpenAIResponses, translator.FormatOpenAI, openaiResponsesToOpenAIRequest, nil)
	translator.Register(translator.FormatOpenAI, translator.FormatOpenAIResponses, openaiToOpenAIResponsesRequest, nil)
}

type responsesToOpenAICollector struct {
	currentAssistantMsg map[string]any
	pendingReasoning    string
	messages            []any
	pendingToolResults  []any
}

func (c *responsesToOpenAICollector) flushAssistant() {
	if c.currentAssistantMsg != nil {
		c.messages = append(c.messages, c.currentAssistantMsg)
		c.currentAssistantMsg = nil
	}
}

func (c *responsesToOpenAICollector) flushToolResults() {
	if len(c.pendingToolResults) > 0 {
		c.messages = append(c.messages, c.pendingToolResults...)
		c.pendingToolResults = nil
	}
}

func parseResponsesImagePart(c map[string]any) map[string]any {
	var url string
	if u, ok := c["image_url"].(string); ok && u != "" {
		url = u
	} else if fid, ok := c["file_id"].(string); ok {
		url = fid
	}

	detail := "auto"
	if d, ok := c["detail"].(string); ok && d != "" {
		detail = d
	}

	return map[string]any{
		"type": schema.OpenaiBlockImageURL,
		"image_url": map[string]any{
			"url":    url,
			"detail": detail,
		},
	}
}

func parseResponsesContentPart(c map[string]any) map[string]any {
	cType, ok := c["type"].(string)
	if !ok {
		return c
	}

	switch cType {
	case schema.ResponsesItemInputText, schema.ResponsesItemOutputText:
		if text, ok := c["text"].(string); ok {
			return map[string]any{"type": schema.OpenaiBlockText, "text": text}
		}
	case schema.ResponsesItemInputImage:
		return parseResponsesImagePart(c)
	default:
		return c
	}

	return nil
}

func (c *responsesToOpenAICollector) handleMessage(item map[string]any) {
	c.flushAssistant()
	c.flushToolResults()

	role, ok := item["role"].(string)
	if !ok {
		role = schema.RoleUser
	}

	contentArr, ok := item["content"].([]any)
	if !ok {
		contentArr = nil
	}

	var newContent []any

	for _, cRaw := range contentArr {
		if cMap, ok := cRaw.(map[string]any); ok && cMap != nil {
			if part := parseResponsesContentPart(cMap); part != nil {
				newContent = append(newContent, part)
			}
		}
	}

	msg := map[string]any{"role": role, "content": newContent}
	if role == schema.RoleAssistant && c.pendingReasoning != "" {
		msg["reasoning_content"] = c.pendingReasoning
		c.pendingReasoning = ""
	}

	c.messages = append(c.messages, msg)
}

func (c *responsesToOpenAICollector) handleFunctionCall(item map[string]any) {
	if c.currentAssistantMsg == nil {
		c.currentAssistantMsg = map[string]any{
			"role":       schema.RoleAssistant,
			"content":    nil,
			"tool_calls": []any{},
		}
		if c.pendingReasoning != "" {
			c.currentAssistantMsg["reasoning_content"] = c.pendingReasoning
			c.pendingReasoning = ""
		}
	}

	name, ok := item["name"].(string)
	if !ok || name == "" {
		return
	}

	callID, ok := item["call_id"].(string)
	if !ok {
		callID = ""
	}

	args, ok := item["arguments"].(string)
	if !ok {
		args = ""
	}

	tc := map[string]any{
		"id":   clampCallID(callID),
		"type": schema.OpenaiBlockFunction,
		"function": map[string]any{
			"name":      name,
			"arguments": args,
		},
	}

	tcArr, ok := c.currentAssistantMsg["tool_calls"].([]any)
	if !ok {
		tcArr = []any{}
	}

	c.currentAssistantMsg["tool_calls"] = append(tcArr, tc)
}

func (c *responsesToOpenAICollector) handleFunctionCallOutput(item map[string]any) {
	c.flushAssistant()
	c.flushToolResults()

	callID, ok := item["call_id"].(string)
	if !ok {
		callID = ""
	}

	output, ok := item["output"].(string)
	if !ok || output == "" {
		output = "{}"
	}

	c.messages = append(c.messages, map[string]any{
		"role":         schema.RoleTool,
		"tool_call_id": clampCallID(callID),
		"content":      output,
	})
}

func (c *responsesToOpenAICollector) handleReasoning(item map[string]any) {
	summary, ok := item["summary"].([]any)
	if !ok {
		return
	}

	var txt string

	for _, s := range summary {
		if sm, ok := s.(map[string]any); ok && sm != nil {
			if t, ok := sm["text"].(string); ok {
				txt += t + "\n"
			}
		}
	}

	if txt != "" {
		if c.pendingReasoning != "" {
			c.pendingReasoning += "\n" + txt
		} else {
			c.pendingReasoning = txt
		}
	}
}

func (c *responsesToOpenAICollector) dispatchInputItem(item map[string]any) {
	itemType, ok := item["type"].(string)
	if !ok || itemType == "" {
		if _, hasRole := item["role"]; hasRole {
			itemType = schema.ResponsesItemMessage
		}
	}

	switch itemType {
	case schema.ResponsesItemMessage:
		c.handleMessage(item)
	case schema.ResponsesItemFunctionCall:
		c.handleFunctionCall(item)
	case schema.ResponsesItemFunctionCallOutput:
		c.handleFunctionCallOutput(item)
	case schema.ResponsesItemReasoning:
		c.handleReasoning(item)
	}
}

func parseResponsesInput(inputItems []any) []any {
	col := &responsesToOpenAICollector{
		currentAssistantMsg: nil,
		pendingReasoning:    "",
		messages:            []any{},
		pendingToolResults:  nil,
	}

	for _, itemRaw := range inputItems {
		if item, ok := itemRaw.(map[string]any); ok && item != nil {
			col.dispatchInputItem(item)
		}
	}

	col.flushAssistant()
	col.flushToolResults()

	return col.messages
}

func parseResponsesTools(tools []any) []any {
	resultTools := make([]any, 0, len(tools))

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok || tool == nil {
			continue
		}

		if _, hasFn := tool["function"].(map[string]any); hasFn {
			resultTools = append(resultTools, toolRaw)
			continue
		}

		name, ok := tool["name"].(string)
		if !ok || name == "" {
			continue
		}

		desc, ok := tool["description"].(string)
		if !ok {
			desc = ""
		}

		params, ok := tool["parameters"].(map[string]any)
		if !ok || params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}

		resultTools = append(resultTools, map[string]any{
			"type": schema.OpenaiBlockFunction,
			"function": map[string]any{
				"name":        name,
				"description": desc,
				"parameters":  params,
			},
		})
	}

	return resultTools
}

func openaiResponsesToOpenAIRequest(_ string, body map[string]any, _ bool, _ map[string]any) map[string]any {
	if _, ok := body["input"]; !ok {
		return body
	}

	result := make(map[string]any, len(body))
	for k, v := range body {
		result[k] = v
	}

	var messages []any
	if instructions, ok := body["instructions"].(string); ok && instructions != "" {
		messages = append(messages, map[string]any{
			"role":    schema.RoleSystem,
			"content": instructions,
		})
	}

	inputItems, ok := body["input"].([]any)
	if ok {
		messages = append(messages, parseResponsesInput(inputItems)...)
	}

	result["messages"] = messages

	if tools, ok := body["tools"].([]any); ok {
		result["tools"] = parseResponsesTools(tools)
	}

	if mot, ok := result["max_output_tokens"].(float64); ok {
		if _, exists := result["max_tokens"]; !exists {
			result["max_tokens"] = mot
		}

		delete(result, "max_output_tokens")
	}

	delete(result, "input")
	delete(result, "instructions")
	delete(result, "include")
	delete(result, "prompt_cache_key")
	delete(result, "store")
	delete(result, "reasoning")
	delete(result, "client_metadata")

	return result
}

func parseOpenAIImageBlockToResponses(block map[string]any) map[string]any {
	var url string

	if iu, ok := block["image_url"].(map[string]any); ok && iu != nil {
		if u, ok := iu["url"].(string); ok {
			url = u
		}
	}

	return map[string]any{
		"type":      schema.ResponsesItemInputImage,
		"image_url": url,
	}
}

func parseOpenAITextFallback(block map[string]any) string {
	if text, ok := block["text"].(string); ok && text != "" {
		return text
	}

	if c, ok := block["content"].(string); ok && c != "" {
		return c
	}

	if textBytes, err := json.Marshal(block); err == nil {
		return string(textBytes)
	}

	return ""
}

func parseSingleOpenAIBlockToResponses(block map[string]any, contentType string) map[string]any {
	btype, ok := block["type"].(string)
	if !ok {
		btype = ""
	}

	if btype == schema.OpenaiBlockText {
		if text, ok := block["text"].(string); ok {
			return map[string]any{"type": contentType, "text": text}
		}

		return nil
	}

	if btype == schema.OpenaiBlockImageURL {
		return parseOpenAIImageBlockToResponses(block)
	}

	return map[string]any{"type": contentType, "text": parseOpenAITextFallback(block)}
}

func parseOpenAIToResponsesContent(content any, contentType string) []any {
	var contentItems []any

	switch c := content.(type) {
	case string:
		contentItems = append(contentItems, map[string]any{"type": contentType, "text": c})
	case []any:
		for _, cRaw := range c {
			block, ok := cRaw.(map[string]any)
			if !ok || block == nil {
				continue
			}

			if item := parseSingleOpenAIBlockToResponses(block, contentType); item != nil {
				contentItems = append(contentItems, item)
			}
		}
	}

	return contentItems
}

func convertAssistantToolCallsToResponses(tcArr []any) []any {
	items := make([]any, 0, len(tcArr))

	for _, tcRaw := range tcArr {
		tc, ok := tcRaw.(map[string]any)
		if !ok || tc == nil {
			continue
		}

		fn, ok := tc["function"].(map[string]any)
		if !ok || fn == nil {
			continue
		}

		name, ok := fn["name"].(string)
		if !ok {
			name = ""
		}

		args, ok := fn["arguments"].(string)
		if !ok {
			args = ""
		}

		id, ok := tc["id"].(string)
		if !ok {
			id = ""
		}

		items = append(items, map[string]any{
			"type":      schema.ResponsesItemFunctionCall,
			"call_id":   clampCallID(id),
			"name":      name,
			"arguments": args,
		})
	}

	return items
}

func convertToolMsgToResponses(msg map[string]any, content any) []any {
	tcID, ok := msg["tool_call_id"].(string)
	if !ok {
		tcID = ""
	}

	var output string
	if s, ok := content.(string); ok {
		output = s
	}

	return []any{
		map[string]any{
			"type":    schema.ResponsesItemFunctionCallOutput,
			"call_id": clampCallID(tcID),
			"output":  output,
		},
	}
}

func convertOpenAIMessageToResponses(msg map[string]any) []any {
	var items []any

	role, ok := msg["role"].(string)
	if !ok {
		role = ""
	}

	content := msg["content"]

	if role == schema.RoleUser || role == schema.RoleAssistant {
		contentType := schema.ResponsesItemInputText
		if role == schema.RoleAssistant {
			contentType = schema.ResponsesItemOutputText
		}

		contentItems := parseOpenAIToResponsesContent(content, contentType)
		if len(contentItems) > 0 {
			items = append(items, map[string]any{
				"type":    schema.ResponsesItemMessage,
				"role":    role,
				"content": contentItems,
			})
		}
	}

	if role == schema.RoleAssistant {
		if tcArr, ok := msg["tool_calls"].([]any); ok {
			items = append(items, convertAssistantToolCallsToResponses(tcArr)...)
		}
	}

	if role == schema.RoleTool {
		items = append(items, convertToolMsgToResponses(msg, content)...)
	}

	return items
}

func parseSingleOpenAIToResponsesTool(tool map[string]any) (map[string]any, bool) {
	t, ok := tool["type"].(string)
	if !ok || t != schema.OpenaiBlockFunction {
		return nil, false
	}

	fn, ok := tool["function"].(map[string]any)
	if !ok || fn == nil {
		return nil, false
	}

	name, ok := fn["name"].(string)
	if !ok {
		name = ""
	}

	desc, ok := fn["description"].(string)
	if !ok {
		desc = ""
	}

	params, ok := fn["parameters"].(map[string]any)
	if !ok || params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}

	return map[string]any{
		"type":        schema.OpenaiBlockFunction,
		"name":        name,
		"description": desc,
		"parameters":  params,
	}, true
}

func parseOpenAIToResponsesTools(tools []any) []any {
	var resultTools []any

	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok || tool == nil {
			continue
		}

		if resTool, valid := parseSingleOpenAIToResponsesTool(tool); valid {
			resultTools = append(resultTools, resTool)
		}
	}

	return resultTools
}

func applyOpenAIToResponsesOptionalParams(result, body map[string]any) {
	if temp, ok := body["temperature"].(float64); ok {
		result["temperature"] = temp
	}

	if mt, ok := body["max_tokens"].(float64); ok {
		result["max_tokens"] = mt
	}

	if tp, ok := body["top_p"].(float64); ok {
		result["top_p"] = tp
	}

	if re, ok := body["reasoning_effort"].(string); ok {
		result["reasoning"] = map[string]any{"effort": re, "summary": "auto"}
	}
}

func processOpenAIMessagesToResponses(messages []any, result map[string]any) {
	var (
		hasSystem bool
		inputArr  []any
	)

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok || msg == nil {
			continue
		}

		role, ok := msg["role"].(string)
		if !ok {
			role = ""
		}

		if (role == schema.RoleSystem || role == schema.RoleDeveloper) && !hasSystem {
			if s, ok := msg["content"].(string); ok {
				result["instructions"] = s
			}

			hasSystem = true

			continue
		}

		inputArr = append(inputArr, convertOpenAIMessageToResponses(msg)...)
	}

	if !hasSystem {
		result["instructions"] = ""
	}

	result["input"] = inputArr
}

func openaiToOpenAIResponsesRequest(model string, body map[string]any, _ bool, _ map[string]any) map[string]any {
	if _, ok := body["input"]; ok {
		result := make(map[string]any, len(body)+2)
		for k, v := range body {
			result[k] = v
		}

		result["model"] = model
		result["stream"] = true

		return result
	}

	result := map[string]any{
		"model":  model,
		"input":  []any{},
		"stream": true,
		"store":  false,
	}

	if messages, ok := body["messages"].([]any); ok {
		processOpenAIMessagesToResponses(messages, result)
	} else {
		result["instructions"] = ""
	}

	if tools, ok := body["tools"].([]any); ok {
		result["tools"] = parseOpenAIToResponsesTools(tools)
	}

	applyOpenAIToResponsesOptionalParams(result, body)

	return result
}
