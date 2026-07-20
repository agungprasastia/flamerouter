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

func ollamaToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	if !state.ResponseCreated {
		state.ResponseCreated = true
		state.ResponseId = "chatcmpl-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		state.Created = time.Now().Unix()
		state.Model, _ = chunk["model"].(string)
		if state.Model == "" {
			state.Model = "ollama"
		}
	}

	meta := map[string]any{
		"id":      state.ResponseId,
		"created": state.Created,
		"model":   state.Model,
	}

	if done, ok := chunk["done"].(bool); ok && done {
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

	message, ok := chunk["message"].(map[string]any)
	if !ok {
		return nil
	}

	content, _ := message["content"].(string)
	thinking, _ := message["thinking"].(string)
	toolCalls, _ := message["tool_calls"].([]any)

	if content == "" && thinking == "" && len(toolCalls) == 0 {
		return nil
	}

	if content != "" {
		state.AccumulatedContent += content
	}
	if thinking != "" {
		state.AccumulatedThinking += thinking
	}

	delta := map[string]any{}
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

	return []map[string]any{buildOllamaChunk(meta, delta, nil)}
}

func buildOllamaChunk(meta, delta, finishReason any) map[string]any {
	return map[string]any{
		"object":  "chat.completion.chunk",
		"created": meta.(map[string]any)["created"],
		"id":      meta.(map[string]any)["id"],
		"model":   meta.(map[string]any)["model"],
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

func convertOllamaToolCalls(toolCalls []any) []any {
	var result []any
	for i, tcRaw := range toolCalls {
		tc, ok := tcRaw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tc["function"].(map[string]any)
		name := ""
		var args any
		if fn != nil {
			name, _ = fn["name"].(string)
			args = fn["arguments"]
		}
		argsStr := "{}"
		switch a := args.(type) {
		case string:
			argsStr = a
		case map[string]any:
			b, _ := json.Marshal(a)
			argsStr = string(b)
		}
		id := ""
		if idVal, ok := tc["id"].(string); ok {
			id = idVal
		}
		if id == "" {
			id = "call_" + strconv.Itoa(i)
		}
		result = append(result, map[string]any{
			"index": i,
			"id":    id,
			"type":  schema.OpenaiBlockFunction,
			"function": map[string]any{
				"name":      name,
				"arguments": argsStr,
			},
		})
	}
	return result
}
