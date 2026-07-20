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
	translator.Register(translator.FormatKiro, translator.FormatOpenAI, nil, kiroToOpenAIResponse)
}

func kiroToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	if obj, ok := chunk["object"].(string); ok && obj == "chat.completion.chunk" {
		if _, ok := chunk["choices"].([]any); ok {
			return []map[string]any{chunk}
		}
	}

	if !state.ResponseCreated {
		state.ResponseCreated = true
		state.ResponseId = "chatcmpl-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		state.Created = time.Now().Unix()
		state.ChunkIndex = 0
	}

	meta := map[string]any{
		"id":      state.ResponseId,
		"created": state.Created,
		"model":   state.Model,
	}

	eventType := ""
	if et, ok := chunk["event"].(string); ok {
		eventType = et
	}
	if et, ok := chunk["event_type"].(string); ok && eventType == "" {
		eventType = et
	}

	if eventType == "assistantResponseEvent" || chunk["assistantResponseEvent"] != nil {
		content := ""
		if evt, ok := chunk["assistantResponseEvent"].(map[string]any); ok {
			content, _ = evt["content"].(string)
		} else if c, ok := chunk["content"].(string); ok {
			content = c
		}
		if content == "" {
			return nil
		}
		delta := map[string]any{}
		if state.ChunkIndex == 0 {
			delta["role"] = schema.RoleAssistant
		}
		delta["content"] = content
		state.ChunkIndex++
		return []map[string]any{buildKiroChunk(meta, delta, nil)}
	}

	if eventType == "reasoningContentEvent" || chunk["reasoningContentEvent"] != nil {
		content := ""
		if evt, ok := chunk["reasoningContentEvent"].(string); ok {
			content = evt
		} else if evt, ok := chunk["reasoningContentEvent"].(map[string]any); ok {
			content, _ = evt["text"].(string)
			if content == "" {
				content, _ = evt["content"].(string)
			}
		}
		if content == "" {
			if c, ok := chunk["content"].(string); ok {
				content = c
			}
		}
		if content == "" {
			return nil
		}
		delta := concerns.ReasoningDelta(content, state.ChunkIndex == 0)
		state.ChunkIndex++
		return []map[string]any{buildKiroChunk(meta, delta, nil)}
	}

	if eventType == "toolUseEvent" || chunk["toolUseEvent"] != nil {
		toolUse, _ := chunk["toolUseEvent"].(map[string]any)
		if toolUse == nil {
			toolUse = chunk
		}
		toolCallId := ""
		if t, ok := toolUse["toolUseId"].(string); ok {
			toolCallId = t
		}
		if toolCallId == "" {
			toolCallId = "call_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		}
		toolName := ""
		if n, ok := toolUse["name"].(string); ok {
			toolName = n
		}
		toolInput := toolUse["input"]
		argsStr := "{}"
		if toolInput != nil {
			b, _ := json.Marshal(toolInput)
			argsStr = string(b)
		}
		delta := map[string]any{}
		if state.ChunkIndex == 0 {
			delta["role"] = schema.RoleAssistant
		}
		delta["tool_calls"] = []any{map[string]any{
			"index": 0,
			"id":    toolCallId,
			"type":  schema.OpenaiBlockFunction,
			"function": map[string]any{
				"name":      toolName,
				"arguments": argsStr,
			},
		}}
		state.ChunkIndex++
		return []map[string]any{buildKiroChunk(meta, delta, nil)}
	}

	if eventType == "messageStopEvent" || eventType == "done" || chunk["messageStopEvent"] != nil {
		finishReason := "stop"
		if state.HadToolUse {
			finishReason = "tool_calls"
		}
		openaiFinish := concerns.ToOpenAIFinish(finishReason, "kiro")
		result := buildKiroChunk(meta, map[string]any{}, openaiFinish)
		if state.RawUsage != nil {
			result["usage"] = state.RawUsage
		}
		return []map[string]any{result}
	}

	if eventType == "usageEvent" || chunk["usageEvent"] != nil {
		usageRaw, ok := chunk["usageEvent"].(map[string]any)
		if !ok {
			usageRaw = chunk
		}
		usage := concerns.ToOpenAIUsage(usageRaw, "kiro")
		if usage != nil {
			state.RawUsage = usage
		}
		return nil
	}

	return nil
}

func buildKiroChunk(meta, delta, finishReason any) map[string]any {
	chunk := map[string]any{
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
	return chunk
}
