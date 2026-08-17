package response

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"flamerouter/internal/translator/schema"
	"strconv"
	"time"
)

func init() {
	translator.Register(translator.FormatOllama, translator.FormatOpenAI, nil, ollamaToOpenAIResponse)
}

func initOllamaState(chunk map[string]any, state *concerns.ResponseState) {
	if state.ResponseCreated {
		return
	}

	state.ResponseCreated = true
	state.ResponseID = "chatcmpl-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	state.Created = time.Now().Unix()

	if m, ok := chunk["model"].(string); ok {
		state.Model = m
	}

	if state.Model == "" {
		state.Model = "ollama"
	}
}

func handleOllamaDone(chunk map[string]any, meta map[string]any, state *concerns.ResponseState) []map[string]any {
	usage := parseOllamaUsage(chunk)

	finishReason := "stop"
	if doneReason, ok := chunk["done_reason"].(string); ok {
		finishReason = concerns.ToOpenAIFinish(doneReason, "ollama")
	}

	if state.HadToolCalls {
		finishReason = "tool_calls"
	}

	result := buildOllamaChunk(meta, map[string]any{}, finishReason)
	if usage != nil {
		result["usage"] = usage
	}

	return []map[string]any{result}
}

func extractOllamaDelta(message map[string]any, state *concerns.ResponseState) map[string]any {
	content := ""
	if c, ok := message["content"].(string); ok {
		content = c
	}

	thinking := ""
	if t, ok := message["thinking"].(string); ok {
		thinking = t
	}

	var toolCalls []any
	if tc, ok := message["tool_calls"].([]any); ok {
		toolCalls = tc
	}

	if content == "" && thinking == "" && len(toolCalls) == 0 {
		return nil
	}

	state.AccumulatedContent += content
	state.AccumulatedThinking += thinking

	delta := make(map[string]any)
	if content != "" {
		delta["content"] = content
	}

	if thinking != "" {
		delta["reasoning_content"] = thinking
	}

	if len(toolCalls) > 0 {
		state.HadToolCalls = true
		delta["tool_calls"] = convertOllamaToolCalls(toolCalls)
	}

	return delta
}

func ollamaToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	initOllamaState(chunk, state)

	meta := map[string]any{
		"id":      state.ResponseID,
		"created": state.Created,
		"model":   state.Model,
	}

	if done, ok := chunk["done"].(bool); ok && done {
		return handleOllamaDone(chunk, meta, state)
	}

	message, ok := chunk["message"].(map[string]any)
	if !ok {
		return nil
	}

	delta := extractOllamaDelta(message, state)
	if delta == nil {
		return nil
	}

	return []map[string]any{buildOllamaChunk(meta, delta, nil)}
}

func buildOllamaChunk(meta map[string]any, delta, finishReason any) map[string]any {
	return map[string]any{
		"object":  "chat.completion.chunk",
		"created": meta["created"],
		"id":      meta["id"],
		"model":   meta["model"],
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason,
		}},
	}
}

func parseOllamaUsage(chunk map[string]any) map[string]any {
	promptTokens := getInt(chunk, "prompt_eval_count")
	evalTokens := getInt(chunk, "eval_count")

	if promptTokens == 0 && evalTokens == 0 {
		return nil
	}

	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": evalTokens,
		"total_tokens":      promptTokens + evalTokens,
	}
}

func parseOllamaToolArgs(args any) string {
	switch a := args.(type) {
	case string:
		return a
	case map[string]any:
		b, err := json.Marshal(a)
		if err == nil {
			return string(b)
		}
	}

	return "{}"
}

func convertSingleOllamaToolCall(i int, tc map[string]any) map[string]any {
	fn, ok := tc["function"].(map[string]any)
	name := ""

	var args any

	if ok && fn != nil {
		if n, nOk := fn["name"].(string); nOk {
			name = n
		}

		args = fn["arguments"]
	}

	argsStr := parseOllamaToolArgs(args)

	id := ""
	if idVal, idOk := tc["id"].(string); idOk {
		id = idVal
	}

	if id == "" {
		id = "call_" + strconv.Itoa(i)
	}

	return map[string]any{
		"index": i,
		"id":    id,
		"type":  schema.OpenaiBlockFunction,
		"function": map[string]any{
			"name":      name,
			"arguments": argsStr,
		},
	}
}

func convertOllamaToolCalls(toolCalls []any) []any {
	result := make([]any, 0, len(toolCalls))

	for i, tcRaw := range toolCalls {
		tc, ok := tcRaw.(map[string]any)
		if !ok {
			continue
		}

		result = append(result, convertSingleOllamaToolCall(i, tc))
	}

	return result
}
