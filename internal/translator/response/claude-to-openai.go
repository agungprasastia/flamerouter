package response

import (
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"time"
)

func init() {
	translator.Register(translator.FormatClaude, translator.FormatOpenAI, nil, claudeToOpenAIResponse)
}

func claudeToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	var results []map[string]any

	event, _ := chunk["type"].(string)
	switch event {
	case "message_start":
		msg, _ := chunk["message"].(map[string]any)
		if msg != nil {
			state.MessageID, _ = msg["id"].(string)
			if state.MessageID == "" {
				state.MessageID = "msg_" + time.Now().Format("20060102150405.000")
			}

			state.Model, _ = msg["model"].(string)
			state.ToolCallIndex = 0

			if usage, ok := msg["usage"].(map[string]any); ok {
				inputTokens := int(usage["input_tokens"].(float64))

				cacheRead := 0
				if v, ok := usage["cache_read_input_tokens"].(float64); ok {
					cacheRead = int(v)
				}

				cacheCreate := 0
				if v, ok := usage["cache_creation_input_tokens"].(float64); ok {
					cacheCreate = int(v)
				}

				promptTokens := inputTokens + cacheRead + cacheCreate

				state.Usage = &concerns.UsageInfo{
					PromptTokens:     promptTokens,
					CompletionTokens: 0,
					TotalTokens:      promptTokens,
					InputTokens:      inputTokens,
					OutputTokens:     0,
				}
				if cacheRead > 0 {
					state.Usage.CacheReadTokens = cacheRead
				}

				if cacheCreate > 0 {
					state.Usage.CacheCreateTokens = cacheCreate
				}
			}
		}

		results = append(results, buildOpenAIChunk(state, map[string]any{"role": "assistant"}, nil))
	case "content_block_start":
		block, _ := chunk["content_block"].(map[string]any)
		if block == nil {
			break
		}

		btype, _ := block["type"].(string)
		switch btype {
		case "text":
			state.TextBlockStarted = true
		case "thinking":
			state.InThinkingBlock = true
			state.CurrentBlockIndex = int(chunk["index"].(float64))
			results = append(results, buildOpenAIChunk(state, map[string]any{"content": "<think>"}, nil))
		case "tool_use":
			idx := state.ToolCallIndex
			state.ToolCallIndex++

			toolName, _ := block["name"].(string)
			if mapped, ok := state.ToolNameMap[toolName]; ok {
				toolName = mapped
			}

			toolCall := &concerns.ToolCallInfo{
				ID:         block["id"].(string),
				Name:       toolName,
				BlockIndex: int(chunk["index"].(float64)),
			}
			state.ToolCalls[int(chunk["index"].(float64))] = toolCall
			results = append(results, buildOpenAIChunk(state, map[string]any{
				"tool_calls": []any{map[string]any{
					"index": idx,
					"id":    toolCall.ID,
					"type":  "function",
					"function": map[string]any{
						"name":      toolName,
						"arguments": "",
					},
				}},
			}, nil))
		}
	case "content_block_delta":
		delta, _ := chunk["delta"].(map[string]any)
		if delta == nil {
			break
		}

		dtype, _ := delta["type"].(string)
		switch dtype {
		case "text_delta":
			if text, ok := delta["text"].(string); ok {
				results = append(results, buildOpenAIChunk(state, map[string]any{"content": text}, nil))
			}
		case "thinking_delta":
			if thinking, ok := delta["thinking"].(string); ok {
				results = append(results, buildOpenAIChunk(state, map[string]any{"content": "<think>" + thinking + "</think>"}, nil))
			}
		case "input_json_delta":
			if pj, ok := delta["partial_json"].(string); ok {
				idx := int(chunk["index"].(float64))
				if toolInfo, ok := state.ToolCalls[idx]; ok {
					state.ToolArgBuffers[idx] += pj
					results = append(results, buildOpenAIChunk(state, map[string]any{
						"tool_calls": []any{map[string]any{
							"index": state.ToolCallIndex - 1,
							"id":    toolInfo.ID,
							"function": map[string]any{
								"arguments": pj,
							},
						}},
					}, nil))
				}
			}
		}
	case "content_block_stop":
		if state.InThinkingBlock {
			results = append(results, buildOpenAIChunk(state, map[string]any{"content": "</think>"}, nil))
			state.InThinkingBlock = false
		}

		state.TextBlockStarted = false
	case "message_delta":
		delta, _ := chunk["delta"].(map[string]any)
		if delta != nil {
			if sr, ok := delta["stop_reason"].(string); ok {
				finishReason := convertClaudeFinish(sr)
				state.FinishReason = finishReason
				finalChunk := buildOpenAIChunk(state, map[string]any{}, &finishReason)

				if state.Usage != nil {
					finalChunk["usage"] = map[string]any{
						"prompt_tokens":     state.Usage.PromptTokens,
						"completion_tokens": state.Usage.CompletionTokens,
						"total_tokens":      state.Usage.TotalTokens,
					}
				}

				results = append(results, finalChunk)
				state.FinishReasonSent = true
			}
		}

		if usage, ok := chunk["usage"].(map[string]any); ok {
			if state.Usage == nil {
				state.Usage = &concerns.UsageInfo{}
			}

			if ot, ok := usage["output_tokens"].(float64); ok {
				state.Usage.CompletionTokens = int(ot)
				state.Usage.OutputTokens = int(ot)
				state.Usage.TotalTokens = state.Usage.PromptTokens + int(ot)
			}
		}
	case "message_stop":
		if !state.FinishReasonSent {
			finishReason := "stop"
			if len(state.ToolCalls) > 0 {
				finishReason = "tool_calls"
			}

			usageObj := map[string]any{}
			if state.Usage != nil {
				usageObj["usage"] = map[string]any{
					"prompt_tokens":     state.Usage.PromptTokens,
					"completion_tokens": state.Usage.CompletionTokens,
					"total_tokens":      state.Usage.TotalTokens,
				}
			}

			ch := buildOpenAIChunk(state, map[string]any{}, &finishReason)
			for k, v := range usageObj {
				ch[k] = v
			}

			results = append(results, ch)
			state.FinishReasonSent = true
		}
	}

	if len(results) == 0 {
		return nil
	}

	return results
}

func buildOpenAIChunk(state *concerns.ResponseState, delta map[string]any, finishReason *string) map[string]any {
	chunk := map[string]any{
		"id":      "chatcmpl-" + state.MessageID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   state.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason,
		}},
	}

	return chunk
}

func convertClaudeFinish(reason string) string {
	switch reason {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}
