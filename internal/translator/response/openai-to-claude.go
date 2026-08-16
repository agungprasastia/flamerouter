package response

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"strconv"
	"strings"
	"time"
)

const ClaudeOAuthToolPrefix = "proxy_"

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatClaude, nil, openaiToClaudeResponse)
}

func openaiToClaudeResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil
	}

	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return nil
	}

	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		return nil
	}

	var results []map[string]any

	if chunkUsage, ok := chunk["usage"].(map[string]any); ok {
		promptTokens := getInt(chunkUsage, "prompt_tokens")
		outputTokens := getInt(chunkUsage, "completion_tokens")
		cacheRead := 0
		cacheCreate := 0

		if details, ok := chunkUsage["prompt_tokens_details"].(map[string]any); ok {
			cacheRead = getInt(details, "cached_tokens")
			cacheCreate = getInt(details, "cache_creation_tokens")
		}

		inputTokens := promptTokens - cacheRead - cacheCreate
		state.Usage = &concerns.UsageInfo{
			InputTokens:              inputTokens,
			OutputTokens:             outputTokens,
			CacheReadInputTokens:     cacheRead,
			CacheCreationInputTokens: cacheCreate,
		}
	}

	if !state.MessageStartSent {
		state.MessageStartSent = true
		state.MessageID, _ = chunk["id"].(string)
		state.MessageID = strings.TrimPrefix(state.MessageID, "chatcmpl-")

		if state.MessageID == "" || state.MessageID == "chat" || len(state.MessageID) < 8 {
			if ext, ok := chunk["extend_fields"].(map[string]any); ok {
				if rid, ok := ext["requestId"].(string); ok && rid != "" {
					state.MessageID = rid
				}
			}
		}

		if state.MessageID == "" {
			state.MessageID = "msg_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		}

		state.Model, _ = chunk["model"].(string)
		if state.Model == "" {
			state.Model = schema.ModelFallback
		}

		state.NextBlockIndex = 0
		results = append(results, map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":            state.MessageID,
				"type":          "message",
				"role":          schema.RoleAssistant,
				"model":         state.Model,
				"content":       []any{},
				"stop_reason":   nil,
				"stop_sequence": nil,
				"usage": map[string]any{
					"input_tokens":  0,
					"output_tokens": 0,
				},
			},
		})
	}

	reasoningContent := concerns.ExtractReasoningText(delta)
	if reasoningContent != "" {
		if state.TextBlockStarted && !state.TextBlockClosed {
			results = append(results, map[string]any{
				"type":  "content_block_stop",
				"index": state.TextBlockIndex,
			})
			state.TextBlockStarted = false
			state.TextBlockClosed = true
		}

		if !state.ThinkingBlockStarted {
			state.ThinkingBlockIndex = state.NextBlockIndex
			state.NextBlockIndex++
			state.ThinkingBlockStarted = true
			results = append(results, map[string]any{
				"type":  "content_block_start",
				"index": state.ThinkingBlockIndex,
				"content_block": map[string]any{
					"type":     schema.ClaudeBlockThinking,
					"thinking": "",
				},
			})
		}

		results = append(results, map[string]any{
			"type":  "content_block_delta",
			"index": state.ThinkingBlockIndex,
			"delta": map[string]any{
				"type":     "thinking_delta",
				"thinking": reasoningContent,
			},
		})
	}

	if content, ok := delta["content"].(string); ok && content != "" {
		if state.ThinkingBlockStarted {
			results = append(results, map[string]any{
				"type":  "content_block_stop",
				"index": state.ThinkingBlockIndex,
			})
			state.ThinkingBlockStarted = false
		}

		if !state.TextBlockStarted {
			state.TextBlockIndex = state.NextBlockIndex
			state.NextBlockIndex++
			state.TextBlockStarted = true
			state.TextBlockClosed = false
			results = append(results, map[string]any{
				"type":  "content_block_start",
				"index": state.TextBlockIndex,
				"content_block": map[string]any{
					"type": schema.ClaudeBlockText,
					"text": "",
				},
			})
		}

		results = append(results, map[string]any{
			"type":  "content_block_delta",
			"index": state.TextBlockIndex,
			"delta": map[string]any{
				"type": "text_delta",
				"text": content,
			},
		})
	}

	if toolCalls, ok := delta["tool_calls"].([]any); ok {
		for _, tcRaw := range toolCalls {
			tc, _ := tcRaw.(map[string]any)
			if tc == nil {
				continue
			}

			idx := 0
			if v, ok := tc["index"].(float64); ok {
				idx = int(v)
			}

			if id, ok := tc["id"].(string); ok && id != "" {
				if _, exists := state.ToolCalls[idx]; !exists {
					if state.ThinkingBlockStarted {
						results = append(results, map[string]any{
							"type":  "content_block_stop",
							"index": state.ThinkingBlockIndex,
						})
						state.ThinkingBlockStarted = false
					}

					if state.TextBlockStarted && !state.TextBlockClosed {
						results = append(results, map[string]any{
							"type":  "content_block_stop",
							"index": state.TextBlockIndex,
						})
						state.TextBlockStarted = false
						state.TextBlockClosed = true
					}

					fn, _ := tc["function"].(map[string]any)
					name := ""

					if fn != nil {
						name, _ = fn["name"].(string)
					}

					state.ToolCalls[idx] = &concerns.ToolCallInfo{
						ID:         id,
						Name:       name,
						BlockIndex: state.NextBlockIndex,
					}
					state.NextBlockIndex++

					toolName := name
					if mapped, ok := state.ToolNameMap[name]; ok {
						toolName = mapped
					}

					toolName = strings.TrimPrefix(toolName, ClaudeOAuthToolPrefix)

					results = append(results, map[string]any{
						"type":  "content_block_start",
						"index": state.ToolCalls[idx].BlockIndex,
						"content_block": map[string]any{
							"type":  schema.ClaudeBlockToolUse,
							"id":    id,
							"name":  toolName,
							"input": map[string]any{},
						},
					})
				}
			}

			if fn, ok := tc["function"].(map[string]any); ok {
				if args, ok := fn["arguments"].(string); ok && args != "" {
					if _, ok := state.ToolCalls[idx]; ok {
						state.ToolArgBuffers[idx] += args
					}
				}
			}
		}
	}

	if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
		if state.ThinkingBlockStarted {
			results = append(results, map[string]any{
				"type":  "content_block_stop",
				"index": state.ThinkingBlockIndex,
			})
			state.ThinkingBlockStarted = false
		}

		if state.TextBlockStarted && !state.TextBlockClosed {
			results = append(results, map[string]any{
				"type":  "content_block_stop",
				"index": state.TextBlockIndex,
			})
			state.TextBlockStarted = false
			state.TextBlockClosed = true
		}

		for _, toolInfo := range state.ToolCalls {
			buffered := state.ToolArgBuffers[toolInfo.BlockIndex]
			if buffered != "" {
				sanitized := sanitizeToolArgs(toolInfo.Name, buffered)
				results = append(results, map[string]any{
					"type":  "content_block_delta",
					"index": toolInfo.BlockIndex,
					"delta": map[string]any{
						"type":         "input_json_delta",
						"partial_json": sanitized,
					},
				})
			}

			results = append(results, map[string]any{
				"type":  "content_block_stop",
				"index": toolInfo.BlockIndex,
			})
		}

		state.FinishReason = fr
		stopReason := convertOpenAIToClaudeFinish(fr)

		finalUsage := state.Usage
		if finalUsage == nil {
			finalUsage = &concerns.UsageInfo{}
		}

		usageMap := map[string]any{
			"input_tokens":  finalUsage.InputTokens,
			"output_tokens": finalUsage.OutputTokens,
		}
		if finalUsage.CacheReadInputTokens > 0 {
			usageMap["cache_read_input_tokens"] = finalUsage.CacheReadInputTokens
		}

		if finalUsage.CacheCreationInputTokens > 0 {
			usageMap["cache_creation_input_tokens"] = finalUsage.CacheCreationInputTokens
		}

		results = append(results, map[string]any{
			"type": "message_delta",
			"delta": map[string]any{
				"stop_reason": stopReason,
			},
			"usage": usageMap,
		})
		results = append(results, map[string]any{
			"type": "message_stop",
		})
	}

	if len(results) == 0 {
		return nil
	}

	return results
}

func sanitizeToolArgs(toolName, argsJson string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJson), &args); err != nil {
		return argsJson
	}

	name := strings.TrimPrefix(toolName, ClaudeOAuthToolPrefix)
	if name == "Read" {
		sanitizeReadArgs(args)
	}

	result, err := json.Marshal(args)
	if err != nil {
		return argsJson
	}

	return string(result)
}

func sanitizeReadArgs(args map[string]any) {
	if limit, ok := args["limit"]; ok {
		switch v := limit.(type) {
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				args["limit"] = parsed
			}
		case float64:
			if v > 2000 {
				args["limit"] = float64(2000)
			}

			if v < 1 {
				delete(args, "limit")
			}
		}
	}

	if offset, ok := args["offset"]; ok {
		switch v := offset.(type) {
		case string:
			if parsed, err := strconv.Atoi(v); err == nil {
				args["offset"] = parsed
			}
		case float64:
			if v < 0 {
				args["offset"] = float64(0)
			}
		}
	}

	if pages, ok := args["pages"]; ok {
		filePath, _ := args["file_path"].(string)
		if !isValidPdfPagesArg(filePath, pages) {
			delete(args, "pages")
		}
	}
}

func isValidPdfPagesArg(filePath string, pages any) bool {
	if filePath == "" || len(filePath) < 4 {
		return false
	}

	if strings.ToLower(filePath[len(filePath)-4:]) != ".pdf" {
		return false
	}

	pagesStr, ok := pages.(string)
	if !ok {
		return false
	}

	for _, c := range pagesStr {
		if (c < '0' || c > '9') && c != '-' {
			return false
		}
	}

	return true
}

func convertOpenAIToClaudeFinish(reason string) string {
	switch reason {
	case "stop":
		return schema.ClaudeStopEndTurn
	case "length":
		return schema.ClaudeStopMaxTokens
	case "tool_calls":
		return schema.ClaudeStopToolUse
	case "content_filter":
		return schema.ClaudeStopEndTurn
	default:
		return schema.ClaudeStopEndTurn
	}
}

func getInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}

	v, ok := m[key]
	if !ok {
		return 0
	}

	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
