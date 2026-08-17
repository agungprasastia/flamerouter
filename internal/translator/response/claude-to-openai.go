package response

import (
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"time"
)

func init() {
	translator.Register(translator.FormatClaude, translator.FormatOpenAI, nil, claudeToOpenAIResponse)
}

func parseClaudeUsage(usage map[string]any, state *concerns.ResponseState) {
	inputTokens := 0
	if it, ok := usage["input_tokens"].(float64); ok {
		inputTokens = int(it)
	}

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
		PromptTokens:             promptTokens,
		CompletionTokens:         0,
		TotalTokens:              promptTokens,
		InputTokens:              inputTokens,
		OutputTokens:             0,
		CacheReadTokens:          cacheRead,
		CacheCreateTokens:        cacheCreate,
		CacheReadInputTokens:     0,
		CacheCreationInputTokens: 0,
	}
}

func handleClaudeMessageStart(chunk map[string]any, state *concerns.ResponseState) map[string]any {
	msg, ok := chunk["message"].(map[string]any)
	if !ok || msg == nil {
		return buildOpenAIChunk(state, map[string]any{"role": "assistant"}, nil)
	}

	if id, idOk := msg["id"].(string); idOk {
		state.MessageID = id
	}

	if state.MessageID == "" {
		state.MessageID = "msg_" + time.Now().Format("20060102150405.000")
	}

	if model, modelOk := msg["model"].(string); modelOk {
		state.Model = model
	}

	state.ToolCallIndex = 0

	if usage, uOk := msg["usage"].(map[string]any); uOk {
		parseClaudeUsage(usage, state)
	}

	return buildOpenAIChunk(state, map[string]any{"role": "assistant"}, nil)
}

func handleClaudeToolStart(block map[string]any, idx int, state *concerns.ResponseState) []map[string]any {
	tcIdx := state.ToolCallIndex
	state.ToolCallIndex++

	toolName := ""
	if tn, ok := block["name"].(string); ok {
		toolName = tn
	}

	if mapped, ok := state.ToolNameMap[toolName]; ok {
		toolName = mapped
	}

	toolID := ""
	if tid, ok := block["id"].(string); ok {
		toolID = tid
	}

	toolCall := &concerns.ToolCallInfo{
		ID:         toolID,
		Name:       toolName,
		BlockIndex: idx,
	}
	state.ToolCalls[idx] = toolCall

	return []map[string]any{buildOpenAIChunk(state, map[string]any{
		"tool_calls": []any{map[string]any{
			"index": tcIdx,
			"id":    toolCall.ID,
			"type":  "function",
			"function": map[string]any{
				"name":      toolName,
				"arguments": "",
			},
		}},
	}, nil)}
}

func handleClaudeContentBlockStart(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	block, ok := chunk["content_block"].(map[string]any)
	if !ok || block == nil {
		return nil
	}

	btype, ok := block["type"].(string)
	if !ok {
		return nil
	}

	idx := 0
	if rawIdx, idxOk := chunk["index"].(float64); idxOk {
		idx = int(rawIdx)
	}

	switch btype {
	case "text":
		state.TextBlockStarted = true

		return nil
	case "thinking":
		state.InThinkingBlock = true
		state.CurrentBlockIndex = idx

		return []map[string]any{buildOpenAIChunk(state, map[string]any{"content": "<think>"}, nil)}
	case "tool_use":
		return handleClaudeToolStart(block, idx, state)
	default:
		return nil
	}
}

func handleClaudeJSONDelta(chunk map[string]any, pj string, state *concerns.ResponseState) []map[string]any {
	idx := 0
	if rawIdx, ok := chunk["index"].(float64); ok {
		idx = int(rawIdx)
	}

	toolInfo, ok := state.ToolCalls[idx]
	if !ok {
		return nil
	}

	state.ToolArgBuffers[idx] += pj

	return []map[string]any{buildOpenAIChunk(state, map[string]any{
		"tool_calls": []any{map[string]any{
			"index": state.ToolCallIndex - 1,
			"id":    toolInfo.ID,
			"function": map[string]any{
				"arguments": pj,
			},
		}},
	}, nil)}
}

func handleClaudeContentBlockDelta(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	delta, ok := chunk["delta"].(map[string]any)
	if !ok || delta == nil {
		return nil
	}

	dtype, ok := delta["type"].(string)
	if !ok {
		return nil
	}

	switch dtype {
	case "text_delta":
		if text, ok := delta["text"].(string); ok {
			return []map[string]any{buildOpenAIChunk(state, map[string]any{"content": text}, nil)}
		}
	case "thinking_delta":
		if thinking, ok := delta["thinking"].(string); ok {
			return []map[string]any{buildOpenAIChunk(state, map[string]any{"content": "<think>" + thinking + "</think>"}, nil)}
		}
	case "input_json_delta":
		if pj, ok := delta["partial_json"].(string); ok {
			return handleClaudeJSONDelta(chunk, pj, state)
		}
	}

	return nil
}

func handleClaudeMessageDelta(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	var results []map[string]any

	if delta, ok := chunk["delta"].(map[string]any); ok && delta != nil {
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
			state.Usage = &concerns.UsageInfo{
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

		if ot, ok := usage["output_tokens"].(float64); ok {
			state.Usage.CompletionTokens = int(ot)
			state.Usage.OutputTokens = int(ot)
			state.Usage.TotalTokens = state.Usage.PromptTokens + int(ot)
		}
	}

	return results
}

func handleClaudeMessageStop(state *concerns.ResponseState) []map[string]any {
	if state.FinishReasonSent {
		return nil
	}

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

	state.FinishReasonSent = true

	return []map[string]any{ch}
}

func handleClaudeBlockStop(state *concerns.ResponseState) []map[string]any {
	state.TextBlockStarted = false
	if state.InThinkingBlock {
		state.InThinkingBlock = false

		return []map[string]any{buildOpenAIChunk(state, map[string]any{"content": "</think>"}, nil)}
	}

	return nil
}

func claudeToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	event, ok := chunk["type"].(string)
	if !ok {
		return nil
	}

	switch event {
	case "message_start":
		return []map[string]any{handleClaudeMessageStart(chunk, state)}
	case "content_block_start":
		return handleClaudeContentBlockStart(chunk, state)
	case "content_block_delta":
		return handleClaudeContentBlockDelta(chunk, state)
	case "content_block_stop":
		return handleClaudeBlockStop(state)
	case "message_delta":
		return handleClaudeMessageDelta(chunk, state)
	case "message_stop":
		return handleClaudeMessageStop(state)
	default:
		return nil
	}
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
