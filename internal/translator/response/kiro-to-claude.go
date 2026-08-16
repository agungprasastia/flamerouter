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

func init() {
	translator.Register(translator.FormatKiro, translator.FormatClaude, nil, kiroToClaudeResponse)
}

func kiroToClaudeResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	data := chunk

	if s, ok := chunk["raw"].(string); ok && s != "" {
		s = strings.TrimSpace(s)
		if s == "" || s == "[DONE]" {
			return nil
		}

		jsonStr := s
		if strings.HasPrefix(jsonStr, "data:") {
			jsonStr = strings.TrimSpace(jsonStr[5:])
		}

		var parsed map[string]any
		if json.Unmarshal([]byte(jsonStr), &parsed) == nil {
			data = parsed
		}
	}

	choices, ok := data["choices"].([]any)
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

	if chunkUsage, ok := data["usage"].(map[string]any); ok {
		inputTokens := getInt(chunkUsage, "prompt_tokens")
		outputTokens := getInt(chunkUsage, "completion_tokens")
		state.Usage = &concerns.UsageInfo{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		}
	}

	if !state.MessageStartSent {
		state.MessageStartSent = true
		state.MessageID = ""

		if id, ok := data["id"].(string); ok {
			state.MessageID = strings.TrimPrefix(id, "chatcmpl-")
		}

		if state.MessageID == "" {
			state.MessageID = "msg_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		}

		state.Model, _ = data["model"].(string)
		if state.Model == "" {
			state.Model = "kiro"
		}

		state.NextBlockIndex = 0
		results = append(results, map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id":      state.MessageID,
				"type":    "message",
				"role":    schema.RoleAssistant,
				"model":   state.Model,
				"content": []any{},
				"usage":   map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		})
	}

	reasoningContent := ""
	if rc, ok := delta["reasoning_content"].(string); ok {
		reasoningContent = rc
	} else if rc, ok := delta["reasoning"].(string); ok {
		reasoningContent = rc
	}

	if reasoningContent != "" {
		if state.TextBlockStarted {
			results = append(results, map[string]any{"type": "content_block_stop", "index": state.TextBlockIndex})
			state.TextBlockStarted = false
		}

		if !state.ThinkingBlockStarted {
			state.ThinkingBlockIndex = state.NextBlockIndex
			state.NextBlockIndex++
			state.ThinkingBlockStarted = true
			results = append(results, map[string]any{
				"type":          "content_block_start",
				"index":         state.ThinkingBlockIndex,
				"content_block": map[string]any{"type": "thinking", "thinking": ""},
			})
		}

		results = append(results, map[string]any{
			"type":  "content_block_delta",
			"index": state.ThinkingBlockIndex,
			"delta": map[string]any{"type": "thinking_delta", "thinking": reasoningContent},
		})
	}

	if content, ok := delta["content"].(string); ok && content != "" {
		if state.ThinkingBlockStarted {
			results = append(results, map[string]any{"type": "content_block_stop", "index": state.ThinkingBlockIndex})
			state.ThinkingBlockStarted = false
		}

		if !state.TextBlockStarted {
			state.TextBlockIndex = state.NextBlockIndex
			state.NextBlockIndex++
			state.TextBlockStarted = true
			results = append(results, map[string]any{
				"type":          "content_block_start",
				"index":         state.TextBlockIndex,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
		}

		results = append(results, map[string]any{
			"type":  "content_block_delta",
			"index": state.TextBlockIndex,
			"delta": map[string]any{"type": "text_delta", "text": content},
		})
	}

	if tc, ok := delta["tool_calls"].([]any); ok {
		if state.KiroToolCalls == nil {
			state.KiroToolCalls = make(map[int]map[string]any)
		}

		for _, tcRaw := range tc {
			toolCall, ok := tcRaw.(map[string]any)
			if !ok {
				continue
			}

			idx := 0
			if i, ok := toolCall["index"].(float64); ok {
				idx = int(i)
			}

			if id, ok := toolCall["id"].(string); ok && id != "" {
				if state.ThinkingBlockStarted {
					results = append(results, map[string]any{"type": "content_block_stop", "index": state.ThinkingBlockIndex})
					state.ThinkingBlockStarted = false
				}

				if state.TextBlockStarted {
					results = append(results, map[string]any{"type": "content_block_stop", "index": state.TextBlockIndex})
					state.TextBlockStarted = false
				}

				blockIndex := state.NextBlockIndex
				state.NextBlockIndex++
				state.KiroToolCalls[idx] = map[string]any{
					"id":         id,
					"name":       "",
					"blockIndex": blockIndex,
				}

				name := ""
				if fn, ok := toolCall["function"].(map[string]any); ok {
					name, _ = fn["name"].(string)
				}

				results = append(results, map[string]any{
					"type":  "content_block_start",
					"index": blockIndex,
					"content_block": map[string]any{
						"type":  "tool_use",
						"id":    id,
						"name":  name,
						"input": map[string]any{},
					},
				})
			}

			if fn, ok := toolCall["function"].(map[string]any); ok {
				if args, ok := fn["arguments"].(string); ok && args != "" {
					if toolInfo, exists := state.KiroToolCalls[idx]; exists {
						blockIndex, _ := toolInfo["blockIndex"].(int)
						results = append(results, map[string]any{
							"type":  "content_block_delta",
							"index": blockIndex,
							"delta": map[string]any{"type": "input_json_delta", "partial_json": args},
						})
					}
				}
			}
		}
	}

	finishReason, _ := choice["finish_reason"].(string)
	if finishReason != "" {
		if state.ThinkingBlockStarted {
			results = append(results, map[string]any{"type": "content_block_stop", "index": state.ThinkingBlockIndex})
			state.ThinkingBlockStarted = false
		}

		if state.TextBlockStarted {
			results = append(results, map[string]any{"type": "content_block_stop", "index": state.TextBlockIndex})
			state.TextBlockStarted = false
		}

		for _, toolInfo := range state.KiroToolCalls {
			blockIndex, _ := toolInfo["blockIndex"].(int)
			results = append(results, map[string]any{"type": "content_block_stop", "index": blockIndex})
		}

		state.FinishReason = finishReason

		usage := state.Usage
		if usage == nil {
			usage = &concerns.UsageInfo{InputTokens: 0, OutputTokens: 0}
		}

		results = append(results, map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": convertKiroFinishReason(finishReason)},
			"usage": map[string]any{"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens},
		})
		results = append(results, map[string]any{"type": "message_stop"})
	}

	if len(results) == 0 {
		return nil
	}

	return results
}

func convertKiroFinishReason(reason string) string {
	switch reason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	default:
		return "end_turn"
	}
}
