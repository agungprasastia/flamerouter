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

func handleKiroBlock(isThinking bool, text string, state *concerns.ResponseState) []map[string]any {
	var results []map[string]any

	if isThinking {
		if state.TextBlockStarted {
			results = append(results, map[string]any{
				"type":  "content_block_stop",
				"index": state.TextBlockIndex,
			})
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
			"delta": map[string]any{"type": "thinking_delta", "thinking": text},
		})

		return results
	}

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
		"delta": map[string]any{"type": "text_delta", "text": text},
	})

	return results
}

func handleKiroToolCallItem(toolCall map[string]any, idx int, id string, state *concerns.ResponseState) []map[string]any {
	var results []map[string]any

	if state.ThinkingBlockStarted {
		state.ThinkingBlockStarted = false
		results = append(results, map[string]any{
			"type":  "content_block_stop",
			"index": state.ThinkingBlockIndex,
		})
	}

	if state.TextBlockStarted {
		state.TextBlockStarted = false
		results = append(results, map[string]any{
			"type":  "content_block_stop",
			"index": state.TextBlockIndex,
		})
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
		if n, nOk := fn["name"].(string); nOk {
			name = n
		}
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

	return results
}

func handleKiroToolCallArgs(toolCall map[string]any, idx int, state *concerns.ResponseState) []map[string]any {
	fn, ok := toolCall["function"].(map[string]any)
	if !ok {
		return nil
	}

	args, ok := fn["arguments"].(string)
	if !ok || args == "" {
		return nil
	}

	toolInfo, exists := state.KiroToolCalls[idx]
	if !exists {
		return nil
	}

	blockIndex, ok := toolInfo["blockIndex"].(int)
	if !ok {
		return nil
	}

	return []map[string]any{
		{
			"type":  "content_block_delta",
			"index": blockIndex,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": args},
		},
	}
}

func handleKiroToolCalls(tc []any, state *concerns.ResponseState) []map[string]any {
	if state.KiroToolCalls == nil {
		state.KiroToolCalls = make(map[int]map[string]any)
	}

	var results []map[string]any

	for _, tcRaw := range tc {
		toolCall, ok := tcRaw.(map[string]any)
		if !ok {
			continue
		}

		idx := 0
		if i, idxOk := toolCall["index"].(float64); idxOk {
			idx = int(i)
		}

		if id, idOk := toolCall["id"].(string); idOk && id != "" {
			results = append(results, handleKiroToolCallItem(toolCall, idx, id, state)...)
		}

		results = append(results, handleKiroToolCallArgs(toolCall, idx, state)...)
	}

	return results
}

func handleKiroFinishReason(finishReason string, state *concerns.ResponseState) []map[string]any {
	var results []map[string]any

	if state.ThinkingBlockStarted {
		results = append(results, map[string]any{"type": "content_block_stop", "index": state.ThinkingBlockIndex})
		state.ThinkingBlockStarted = false
	}

	if state.TextBlockStarted {
		results = append(results, map[string]any{"type": "content_block_stop", "index": state.TextBlockIndex})
		state.TextBlockStarted = false
	}

	for _, toolInfo := range state.KiroToolCalls {
		if blockIndex, ok := toolInfo["blockIndex"].(int); ok {
			results = append(results, map[string]any{"type": "content_block_stop", "index": blockIndex})
		}
	}

	state.FinishReason = finishReason

	usage := state.Usage
	if usage == nil {
		usage = &concerns.UsageInfo{
			PromptTokens:             0,
			CompletionTokens:         0,
			TotalTokens:              0,
			InputTokens:              0,
			OutputTokens:             0,
			CacheReadTokens:          0,
			CacheCreateTokens:        0,
			CacheReadInputTokens:     0,
			CacheCreationInputTokens: 0,
		}
	}

	results = append(results, map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": convertKiroFinishReason(finishReason)},
		"usage": map[string]any{"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens},
	})
	results = append(results, map[string]any{"type": "message_stop"})

	return results
}

func parseKiroRawChunk(chunk map[string]any) map[string]any {
	s, ok := chunk["raw"].(string)
	if !ok || s == "" {
		return chunk
	}

	s = strings.TrimSpace(s)
	if s == "" || s == "[DONE]" {
		return nil
	}

	jsonStr := s
	if strings.HasPrefix(jsonStr, "data:") {
		jsonStr = strings.TrimSpace(jsonStr[5:])
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
		return parsed
	}

	return chunk
}

func handleKiroMessageStart(data map[string]any, state *concerns.ResponseState) []map[string]any {
	if state.MessageStartSent {
		return nil
	}

	state.MessageStartSent = true
	state.MessageID = ""

	if id, ok := data["id"].(string); ok {
		state.MessageID = strings.TrimPrefix(id, "chatcmpl-")
	}

	if state.MessageID == "" {
		state.MessageID = "msg_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	if model, ok := data["model"].(string); ok {
		state.Model = model
	}

	if state.Model == "" {
		state.Model = "kiro"
	}

	state.NextBlockIndex = 0

	return []map[string]any{
		{
			"type": "message_start",
			"message": map[string]any{
				"id":      state.MessageID,
				"type":    "message",
				"role":    schema.RoleAssistant,
				"model":   state.Model,
				"content": []any{},
				"usage":   map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		},
	}
}

func parseKiroUsage(data map[string]any, state *concerns.ResponseState) {
	chunkUsage, ok := data["usage"].(map[string]any)
	if !ok {
		return
	}

	inputTokens := getInt(chunkUsage, "prompt_tokens")
	outputTokens := getInt(chunkUsage, "completion_tokens")
	state.Usage = &concerns.UsageInfo{
		PromptTokens:             inputTokens + outputTokens,
		CompletionTokens:         outputTokens,
		TotalTokens:              inputTokens + outputTokens,
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CacheReadTokens:          0,
		CacheCreateTokens:        0,
		CacheReadInputTokens:     0,
		CacheCreationInputTokens: 0,
	}
}

func extractKiroReasoning(delta map[string]any) string {
	if rc, ok := delta["reasoning_content"].(string); ok {
		return rc
	}

	if rc, ok := delta["reasoning"].(string); ok {
		return rc
	}

	return ""
}

func handleKiroDeltas(delta map[string]any, state *concerns.ResponseState) []map[string]any {
	var results []map[string]any

	reasoningContent := extractKiroReasoning(delta)
	if reasoningContent != "" {
		results = append(results, handleKiroBlock(true, reasoningContent, state)...)
	}

	if content, ok := delta["content"].(string); ok && content != "" {
		results = append(results, handleKiroBlock(false, content, state)...)
	}

	if tc, ok := delta["tool_calls"].([]any); ok {
		results = append(results, handleKiroToolCalls(tc, state)...)
	}

	return results
}

func extractKiroChoice(data map[string]any) (map[string]any, map[string]any, bool) {
	choices, ok := data["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, nil, false
	}

	choice, ok := choices[0].(map[string]any)
	if !ok || choice == nil {
		return nil, nil, false
	}

	delta, ok := choice["delta"].(map[string]any)
	if !ok || delta == nil {
		return nil, nil, false
	}

	return choice, delta, true
}

func kiroToClaudeResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	data := parseKiroRawChunk(chunk)
	if data == nil {
		return nil
	}

	choice, delta, ok := extractKiroChoice(data)
	if !ok {
		return nil
	}

	parseKiroUsage(data, state)

	results := handleKiroMessageStart(data, state)
	results = append(results, handleKiroDeltas(delta, state)...)

	if finishReason, fOk := choice["finish_reason"].(string); fOk && finishReason != "" {
		results = append(results, handleKiroFinishReason(finishReason, state)...)
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
