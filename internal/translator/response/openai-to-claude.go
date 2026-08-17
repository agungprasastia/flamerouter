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

// ClaudeOAuthToolPrefix is the prefix used for Claude OAuth tools.
const ClaudeOAuthToolPrefix = "proxy_"

func init() {
	translator.Register(translator.FormatOpenAI, translator.FormatClaude, nil, openaiToClaudeResponse)
}

func parseOpenAIUsage(chunk map[string]any, state *concerns.ResponseState) {
	chunkUsage, ok := chunk["usage"].(map[string]any)
	if !ok {
		return
	}

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
		PromptTokens:             promptTokens,
		CompletionTokens:         outputTokens,
		TotalTokens:              promptTokens + outputTokens,
		InputTokens:              inputTokens,
		OutputTokens:             outputTokens,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheCreate,
		CacheReadTokens:          0,
		CacheCreateTokens:        0,
	}
}

func resolveClaudeMessageID(chunk map[string]any, currentID string) string {
	msgID := currentID
	if id, idOk := chunk["id"].(string); idOk {
		msgID = id
	}

	msgID = strings.TrimPrefix(msgID, "chatcmpl-")

	if msgID == "" || msgID == "chat" || len(msgID) < 8 {
		if ext, ok := chunk["extend_fields"].(map[string]any); ok {
			if rid, ok := ext["requestId"].(string); ok && rid != "" {
				msgID = rid
			}
		}
	}

	if msgID == "" {
		msgID = "msg_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	return msgID
}

func handleOpenAIToClaudeMessageStart(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if state.MessageStartSent {
		return nil
	}

	state.MessageStartSent = true
	state.MessageID = resolveClaudeMessageID(chunk, state.MessageID)

	if model, modelOk := chunk["model"].(string); modelOk {
		state.Model = model
	}

	if state.Model == "" {
		state.Model = schema.ModelFallback
	}

	state.NextBlockIndex = 0

	return []map[string]any{
		{
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
		},
	}
}

func handleClaudeReasoning(delta map[string]any, state *concerns.ResponseState) []map[string]any {
	reasoningContent := concerns.ExtractReasoningText(delta)
	if reasoningContent == "" {
		return nil
	}

	var results []map[string]any

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

	return results
}

func handleClaudeContent(delta map[string]any, state *concerns.ResponseState) []map[string]any {
	content, ok := delta["content"].(string)
	if !ok || content == "" {
		return nil
	}

	var results []map[string]any

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

	return results
}

func handleClaudeToolCallStart(tc map[string]any, idx int, id string, state *concerns.ResponseState) []map[string]any {
	var results []map[string]any

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

	name := ""

	if fn, ok := tc["function"].(map[string]any); ok && fn != nil {
		if n, nOk := fn["name"].(string); nOk {
			name = n
		}
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

	return results
}

func processSingleClaudeToolCall(tc map[string]any, state *concerns.ResponseState) []map[string]any {
	idx := 0
	if v, vOk := tc["index"].(float64); vOk {
		idx = int(v)
	}

	var results []map[string]any

	if id, idOk := tc["id"].(string); idOk && id != "" {
		if _, exists := state.ToolCalls[idx]; !exists {
			results = append(results, handleClaudeToolCallStart(tc, idx, id, state)...)
		}
	}

	if fn, fnOk := tc["function"].(map[string]any); fnOk {
		if args, argsOk := fn["arguments"].(string); argsOk && args != "" {
			if _, exists := state.ToolCalls[idx]; exists {
				state.ToolArgBuffers[idx] += args
			}
		}
	}

	return results
}

func handleClaudeToolCalls(toolCalls []any, state *concerns.ResponseState) []map[string]any {
	results := make([]map[string]any, 0, len(toolCalls))

	for _, tcRaw := range toolCalls {
		tc, ok := tcRaw.(map[string]any)
		if !ok || tc == nil {
			continue
		}

		results = append(results, processSingleClaudeToolCall(tc, state)...)
	}

	return results
}

func closeClaudeContentBlocks(state *concerns.ResponseState) []map[string]any {
	results := make([]map[string]any, 0, len(state.ToolCalls)+2)

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

	return results
}

func handleClaudeFinishReason(fr string, state *concerns.ResponseState) []map[string]any {
	results := closeClaudeContentBlocks(state)

	state.FinishReason = fr
	stopReason := convertOpenAIToClaudeFinish(fr)

	finalUsage := state.Usage
	if finalUsage == nil {
		finalUsage = &concerns.UsageInfo{
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

	return results
}

func extractOpenAIChoice(chunk map[string]any) (map[string]any, map[string]any, bool) {
	choices, ok := chunk["choices"].([]any)
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

func openaiToClaudeResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	choice, delta, ok := extractOpenAIChoice(chunk)
	if !ok {
		return nil
	}

	parseOpenAIUsage(chunk, state)

	results := handleOpenAIToClaudeMessageStart(chunk, state)
	results = append(results, handleClaudeReasoning(delta, state)...)
	results = append(results, handleClaudeContent(delta, state)...)

	if toolCalls, tcOk := delta["tool_calls"].([]any); tcOk {
		results = append(results, handleClaudeToolCalls(toolCalls, state)...)
	}

	if fr, frOk := choice["finish_reason"].(string); frOk && fr != "" {
		results = append(results, handleClaudeFinishReason(fr, state)...)
	}

	if len(results) == 0 {
		return nil
	}

	return results
}

func sanitizeToolArgs(toolName, argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return argsJSON
	}

	name := strings.TrimPrefix(toolName, ClaudeOAuthToolPrefix)
	if name == "Read" {
		sanitizeReadArgs(args)
	}

	result, err := json.Marshal(args)
	if err != nil {
		return argsJSON
	}

	return string(result)
}

func sanitizeReadLimit(args map[string]any) {
	limit, ok := args["limit"]
	if !ok {
		return
	}

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

func sanitizeReadOffset(args map[string]any) {
	offset, ok := args["offset"]
	if !ok {
		return
	}

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

func sanitizeReadArgs(args map[string]any) {
	sanitizeReadLimit(args)
	sanitizeReadOffset(args)

	if pages, ok := args["pages"]; ok {
		filePath := ""
		if fp, fpOk := args["file_path"].(string); fpOk {
			filePath = fp
		}

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
