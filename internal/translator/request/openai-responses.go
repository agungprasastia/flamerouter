package request

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/schema"
)

const maxCallIDLen = 64

func clampCallId(id string) string {
	if len(id) > maxCallIDLen {
		return id[:maxCallIDLen]
	}

	return id
}

func init() {
	translator.Register(translator.FormatOpenAIResponses, translator.FormatOpenAI, openaiResponsesToOpenAIRequest, nil)
	translator.Register(translator.FormatOpenAI, translator.FormatOpenAIResponses, openaiToOpenAIResponsesRequest, nil)
}

func openaiResponsesToOpenAIRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	if _, ok := body["input"]; !ok {
		return body
	}

	result := make(map[string]any)
	for k, v := range body {
		result[k] = v
	}

	result["messages"] = []any{}

	if instructions, ok := body["instructions"].(string); ok && instructions != "" {
		messages := result["messages"].([]any)
		messages = append(messages, map[string]any{
			"role":    schema.RoleSystem,
			"content": instructions,
		})
		result["messages"] = messages
	}

	var currentAssistantMsg map[string]any

	var pendingToolResults []any

	var pendingReasoning string

	inputItems, ok := body["input"].([]any)
	if !ok {
		return body
	}

	for _, itemRaw := range inputItems {
		item, ok := itemRaw.(map[string]any)
		if !ok {
			continue
		}

		itemType, _ := item["type"].(string)
		if itemType == "" {
			if _, hasRole := item["role"]; hasRole {
				itemType = schema.ResponsesItemMessage
			}
		}

		switch itemType {
		case schema.ResponsesItemMessage:
			if currentAssistantMsg != nil {
				messages := result["messages"].([]any)
				messages = append(messages, currentAssistantMsg)
				result["messages"] = messages
				currentAssistantMsg = nil
			}

			if len(pendingToolResults) > 0 {
				messages := result["messages"].([]any)
				messages = append(messages, pendingToolResults...)
				result["messages"] = messages
				pendingToolResults = nil
			}

			role, _ := item["role"].(string)
			contentArr, _ := item["content"].([]any)

			var newContent []any

			for _, cRaw := range contentArr {
				c, ok := cRaw.(map[string]any)
				if !ok {
					continue
				}

				cType, _ := c["type"].(string)
				switch cType {
				case schema.ResponsesItemInputText:
					if text, ok := c["text"].(string); ok {
						newContent = append(newContent, map[string]any{"type": schema.OpenaiBlockText, "text": text})
					}
				case schema.ResponsesItemOutputText:
					if text, ok := c["text"].(string); ok {
						newContent = append(newContent, map[string]any{"type": schema.OpenaiBlockText, "text": text})
					}
				case schema.ResponsesItemInputImage:
					url, _ := c["image_url"].(string)
					if url == "" {
						url, _ = c["file_id"].(string)
					}

					detail, _ := c["detail"].(string)
					if detail == "" {
						detail = "auto"
					}

					newContent = append(newContent, map[string]any{
						"type": schema.OpenaiBlockImageUrl,
						"image_url": map[string]any{
							"url":    url,
							"detail": detail,
						},
					})
				default:
					newContent = append(newContent, c)
				}
			}

			msg := map[string]any{"role": role, "content": newContent}
			if role == schema.RoleAssistant && pendingReasoning != "" {
				msg["reasoning_content"] = pendingReasoning
				pendingReasoning = ""
			}

			messages := result["messages"].([]any)
			messages = append(messages, msg)
			result["messages"] = messages

		case schema.ResponsesItemFunctionCall:
			if currentAssistantMsg == nil {
				currentAssistantMsg = map[string]any{
					"role":       schema.RoleAssistant,
					"content":    nil,
					"tool_calls": []any{},
				}
				if pendingReasoning != "" {
					currentAssistantMsg["reasoning_content"] = pendingReasoning
					pendingReasoning = ""
				}
			}

			name, _ := item["name"].(string)
			if name == "" {
				continue
			}

			callID, _ := item["call_id"].(string)
			args, _ := item["arguments"].(string)
			tc := map[string]any{
				"id":   clampCallId(callID),
				"type": schema.OpenaiBlockFunction,
				"function": map[string]any{
					"name":      name,
					"arguments": args,
				},
			}
			tcArr := currentAssistantMsg["tool_calls"].([]any)
			tcArr = append(tcArr, tc)
			currentAssistantMsg["tool_calls"] = tcArr

		case schema.ResponsesItemFunctionCallOutput:
			if currentAssistantMsg != nil {
				messages := result["messages"].([]any)
				messages = append(messages, currentAssistantMsg)
				result["messages"] = messages
				currentAssistantMsg = nil
			}

			if len(pendingToolResults) > 0 {
				messages := result["messages"].([]any)
				messages = append(messages, pendingToolResults...)
				result["messages"] = messages
				pendingToolResults = nil
			}

			callID, _ := item["call_id"].(string)

			output, _ := item["output"].(string)
			if output == "" {
				output = "{}"
			}

			messages := result["messages"].([]any)
			messages = append(messages, map[string]any{
				"role":         schema.RoleTool,
				"tool_call_id": clampCallId(callID),
				"content":      output,
			})
			result["messages"] = messages

		case schema.ResponsesItemReasoning:
			if summary, ok := item["summary"].([]any); ok {
				var txt string

				for _, s := range summary {
					if sm, ok := s.(map[string]any); ok {
						if t, ok := sm["text"].(string); ok {
							txt += t + "\n"
						}
					}
				}

				if txt != "" {
					if pendingReasoning != "" {
						pendingReasoning += "\n" + txt
					} else {
						pendingReasoning = txt
					}
				}
			}
		}
	}

	if currentAssistantMsg != nil {
		messages := result["messages"].([]any)
		messages = append(messages, currentAssistantMsg)
		result["messages"] = messages
	}

	if len(pendingToolResults) > 0 {
		messages := result["messages"].([]any)
		messages = append(messages, pendingToolResults...)
		result["messages"] = messages
	}

	if tools, ok := body["tools"].([]any); ok {
		var resultTools []any

		for _, toolRaw := range tools {
			tool, ok := toolRaw.(map[string]any)
			if !ok {
				continue
			}

			if _, ok := tool["function"].(map[string]any); ok {
				resultTools = append(resultTools, toolRaw)
				continue
			}

			name, _ := tool["name"].(string)
			if name == "" {
				continue
			}

			desc, _ := tool["description"].(string)

			params, _ := tool["parameters"].(map[string]any)
			if params == nil {
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

		result["tools"] = resultTools
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

func openaiToOpenAIResponsesRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
	if _, ok := body["input"]; ok {
		result := make(map[string]any)
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

	messages, _ := body["messages"].([]any)

	var hasSystem bool

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)
		content := msg["content"]

		if role == schema.RoleSystem || role == schema.RoleDeveloper {
			if !hasSystem {
				if s, ok := content.(string); ok {
					result["instructions"] = s
				}

				hasSystem = true
			}

			continue
		}

		if role == schema.RoleUser || role == schema.RoleAssistant {
			contentType := schema.ResponsesItemInputText
			if role == schema.RoleAssistant {
				contentType = schema.ResponsesItemOutputText
			}

			var contentItems []any
			switch c := content.(type) {
			case string:
				contentItems = append(contentItems, map[string]any{"type": contentType, "text": c})
			case []any:
				for _, cRaw := range c {
					block, ok := cRaw.(map[string]any)
					if !ok {
						continue
					}

					btype, _ := block["type"].(string)
					switch btype {
					case schema.OpenaiBlockText:
						if text, ok := block["text"].(string); ok {
							contentItems = append(contentItems, map[string]any{"type": contentType, "text": text})
						}
					case schema.OpenaiBlockImageUrl:
						var url string
						if iu, ok := block["image_url"].(map[string]any); ok {
							url, _ = iu["url"].(string)
						}

						contentItems = append(contentItems, map[string]any{
							"type":      schema.ResponsesItemInputImage,
							"image_url": url,
						})
					default:
						text, _ := block["text"].(string)
						if text == "" {
							text, _ = block["content"].(string)
						}

						if text == "" {
							textBytes, _ := json.Marshal(block)
							text = string(textBytes)
						}

						contentItems = append(contentItems, map[string]any{"type": contentType, "text": text})
					}
				}
			}

			if len(contentItems) > 0 {
				inputArr := result["input"].([]any)
				inputArr = append(inputArr, map[string]any{
					"type":    schema.ResponsesItemMessage,
					"role":    role,
					"content": contentItems,
				})
				result["input"] = inputArr
			}
		}

		if role == schema.RoleAssistant {
			if tcArr, ok := msg["tool_calls"].([]any); ok {
				inputArr := result["input"].([]any)

				for _, tcRaw := range tcArr {
					tc, ok := tcRaw.(map[string]any)
					if !ok {
						continue
					}

					fn, _ := tc["function"].(map[string]any)
					name, _ := fn["name"].(string)
					args, _ := fn["arguments"].(string)
					id, _ := tc["id"].(string)
					inputArr = append(inputArr, map[string]any{
						"type":      schema.ResponsesItemFunctionCall,
						"call_id":   clampCallId(id),
						"name":      name,
						"arguments": args,
					})
				}

				result["input"] = inputArr
			}
		}

		if role == schema.RoleTool {
			tcID, _ := msg["tool_call_id"].(string)

			var output string
			if s, ok := content.(string); ok {
				output = s
			}

			inputArr := result["input"].([]any)
			inputArr = append(inputArr, map[string]any{
				"type":    schema.ResponsesItemFunctionCallOutput,
				"call_id": clampCallId(tcID),
				"output":  output,
			})
			result["input"] = inputArr
		}
	}

	if !hasSystem {
		result["instructions"] = ""
	}

	if tools, ok := body["tools"].([]any); ok {
		var resultTools []any

		for _, toolRaw := range tools {
			tool, ok := toolRaw.(map[string]any)
			if !ok {
				continue
			}

			if t, ok := tool["type"].(string); ok && t == schema.OpenaiBlockFunction {
				if fn, ok := tool["function"].(map[string]any); ok {
					name, _ := fn["name"].(string)
					desc, _ := fn["description"].(string)

					params, _ := fn["parameters"].(map[string]any)
					if params == nil {
						params = map[string]any{"type": "object", "properties": map[string]any{}}
					}

					resultTools = append(resultTools, map[string]any{
						"type":        schema.OpenaiBlockFunction,
						"name":        name,
						"description": desc,
						"parameters":  params,
					})
				}
			}
		}

		result["tools"] = resultTools
	}

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

	return result
}
