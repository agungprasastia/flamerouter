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

func openaiToCursorRequest(model string, body map[string]any, stream bool, credentials map[string]any) map[string]any {
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

func convertCursorMessages(body map[string]any) []any {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return []any{}
	}

	toolCallMeta := make(map[string]string)

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)
		if role == schema.RoleAssistant {
			if tc, ok := msg["tool_calls"].([]any); ok {
				for _, tcRaw := range tc {
					toolCall, ok := tcRaw.(map[string]any)
					if !ok {
						continue
					}

					id, _ := toolCall["id"].(string)
					fn, _ := toolCall["function"].(map[string]any)

					name, _ := fn["name"].(string)
					if id != "" {
						toolCallMeta[id] = name
					}
				}
			}

			if arr, ok := msg["content"].([]any); ok {
				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}

					if block["type"] == "tool_use" {
						id, _ := block["id"].(string)
						name, _ := block["name"].(string)

						if id != "" {
							toolCallMeta[id] = name
						}
					}
				}
			}
		}
	}

	var result []any

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		role, _ := msg["role"].(string)

		if role == schema.RoleSystem {
			text := extractCursorText(msg["content"])
			result = append(result, map[string]any{
				"role":    schema.RoleUser,
				"content": "[System Instructions]\n" + text,
			})

			continue
		}

		if role == schema.RoleTool {
			text := extractCursorText(msg["content"])
			tcid, _ := msg["tool_call_id"].(string)

			toolName := toolCallMeta[tcid]
			if toolName == "" {
				toolName = "tool"
			}

			result = append(result, map[string]any{
				"role":    schema.RoleUser,
				"content": buildCursorToolResultBlock(toolName, tcid, text),
			})

			continue
		}

		if role == schema.RoleUser {
			if arr, ok := msg["content"].([]any); ok {
				var parts []string

				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}

					btype, _ := block["type"].(string)
					if btype == "text" || btype == schema.ClaudeBlockText {
						if text, ok := block["text"].(string); ok {
							parts = append(parts, text)
						}
					} else if btype == "tool_result" {
						tuid, _ := block["tool_use_id"].(string)

						toolName := toolCallMeta[tuid]
						if toolName == "" {
							toolName = "tool"
						}

						var resultText string
						if c, ok := block["content"].(string); ok {
							resultText = c
						} else if cArr, ok := block["content"].([]any); ok {
							for _, cBlock := range cArr {
								if cm, ok := cBlock.(map[string]any); ok {
									if t, ok := cm["text"].(string); ok {
										resultText += t
									}
								}
							}
						}

						parts = append(parts, buildCursorToolResultBlock(toolName, tuid, resultText))
					}
				}

				joined := strings.Join(parts, "\n")
				if joined != "" {
					result = append(result, map[string]any{
						"role":    schema.RoleUser,
						"content": joined,
					})
				}

				continue
			}
		}

		if role == schema.RoleAssistant {
			text := extractCursorText(msg["content"])

			if tc, ok := msg["tool_calls"].([]any); ok && len(tc) > 0 {
				var cleanTools []any

				for _, tcRaw := range tc {
					toolCall, ok := tcRaw.(map[string]any)
					if !ok {
						continue
					}

					cleanTools = append(cleanTools, toolCall)
				}

				result = append(result, map[string]any{
					"role":       schema.RoleAssistant,
					"content":    text,
					"tool_calls": cleanTools,
				})
			} else if arr, ok := msg["content"].([]any); ok {
				var extractedTools []any

				for _, blockRaw := range arr {
					block, ok := blockRaw.(map[string]any)
					if !ok {
						continue
					}

					if block["type"] == "tool_use" {
						id, _ := block["id"].(string)
						name, _ := block["name"].(string)
						inputJSON, _ := block["input"].(map[string]any)
						inputStr := "{}"

						if inputJSON != nil {
							b, _ := jsonMarshal(inputJSON)
							inputStr = string(b)
						}

						if id != "" {
							extractedTools = append(extractedTools, map[string]any{
								"id":   id,
								"type": schema.OpenaiBlockFunction,
								"function": map[string]any{
									"name":      name,
									"arguments": inputStr,
								},
							})
						}
					}
				}

				if len(extractedTools) > 0 {
					result = append(result, map[string]any{
						"role":       schema.RoleAssistant,
						"content":    text,
						"tool_calls": extractedTools,
					})
				} else if text != "" {
					result = append(result, map[string]any{
						"role":    schema.RoleAssistant,
						"content": text,
					})
				}
			} else if text != "" {
				result = append(result, map[string]any{
					"role":    schema.RoleAssistant,
					"content": text,
				})
			}

			continue
		}

		text := extractCursorText(msg["content"])
		if text != "" {
			result = append(result, map[string]any{
				"role":    role,
				"content": text,
			})
		}
	}

	return result
}

func extractCursorText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}

	if arr, ok := content.([]any); ok {
		var parts []string

		for _, blockRaw := range arr {
			block, ok := blockRaw.(map[string]any)
			if !ok {
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

	return ""
}

func buildCursorToolResultBlock(toolName, toolCallId, resultText string) string {
	clean := sanitizeCursorText(resultText)

	var b strings.Builder

	b.WriteString("<tool_result>\n")
	b.WriteString("<tool_name>")
	b.WriteString(xmlEscape(toolName))
	b.WriteString("</tool_name>\n")
	b.WriteString("<tool_call_id>")
	b.WriteString(xmlEscape(toolCallId))
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
		if r >= 0x00 && r <= 0x08 || r == 0x0B || r == 0x0C || r >= 0x0E && r <= 0x1F || r == 0x7F {
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

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
