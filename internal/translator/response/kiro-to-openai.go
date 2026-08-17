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

func handleKiroAssistantResponse(chunk map[string]any, meta map[string]any, state *concerns.ResponseState) []map[string]any {
	content := ""

	if evt, ok := chunk["assistantResponseEvent"].(map[string]any); ok {
		if c, ok := evt["content"].(string); ok {
			content = c
		}
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

func handleKiroReasoningEvent(chunk map[string]any, meta map[string]any, state *concerns.ResponseState) []map[string]any {
	content := ""
	if evt, ok := chunk["reasoningContentEvent"].(string); ok {
		content = evt
	} else if evt, ok := chunk["reasoningContentEvent"].(map[string]any); ok {
		if t, ok := evt["text"].(string); ok && t != "" {
			content = t
		} else if c, ok := evt["content"].(string); ok {
			content = c
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

func handleKiroToolUseEvent(chunk map[string]any, meta map[string]any, state *concerns.ResponseState) []map[string]any {
	toolUse, ok := chunk["toolUseEvent"].(map[string]any)
	if !ok || toolUse == nil {
		toolUse = chunk
	}

	toolCallID := ""
	if t, ok := toolUse["toolUseId"].(string); ok {
		toolCallID = t
	}

	if toolCallID == "" {
		toolCallID = "call_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	toolName := ""
	if n, ok := toolUse["name"].(string); ok {
		toolName = n
	}

	toolInput := toolUse["input"]
	argsStr := "{}"

	if toolInput != nil {
		b, err := json.Marshal(toolInput)
		if err == nil {
			argsStr = string(b)
		}
	}

	delta := map[string]any{}
	if state.ChunkIndex == 0 {
		delta["role"] = schema.RoleAssistant
	}

	delta["tool_calls"] = []any{map[string]any{
		"index": 0,
		"id":    toolCallID,
		"type":  schema.OpenaiBlockFunction,
		"function": map[string]any{
			"name":      toolName,
			"arguments": argsStr,
		},
	}}
	state.ChunkIndex++

	return []map[string]any{buildKiroChunk(meta, delta, nil)}
}

func handleKiroMessageStop(meta map[string]any, state *concerns.ResponseState) []map[string]any {
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

func handleKiroUsage(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
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

func initKiroState(state *concerns.ResponseState) map[string]any {
	if !state.ResponseCreated {
		state.ResponseCreated = true
		state.ResponseID = "chatcmpl-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
		state.Created = time.Now().Unix()
		state.ChunkIndex = 0
	}

	return map[string]any{
		"id":      state.ResponseID,
		"created": state.Created,
		"model":   state.Model,
	}
}

func extractKiroEventType(chunk map[string]any) string {
	if et, ok := chunk["event"].(string); ok && et != "" {
		return et
	}

	if et, ok := chunk["event_type"].(string); ok && et != "" {
		return et
	}

	for _, key := range []string{"assistantResponseEvent", "reasoningContentEvent", "toolUseEvent", "messageStopEvent", "usageEvent"} {
		if chunk[key] != nil {
			return key
		}
	}

	return ""
}

func dispatchKiroEvent(eventType string, chunk, meta map[string]any, state *concerns.ResponseState) []map[string]any {
	switch eventType {
	case "assistantResponseEvent":
		return handleKiroAssistantResponse(chunk, meta, state)
	case "reasoningContentEvent":
		return handleKiroReasoningEvent(chunk, meta, state)
	case "toolUseEvent":
		return handleKiroToolUseEvent(chunk, meta, state)
	case "messageStopEvent", "done":
		return handleKiroMessageStop(meta, state)
	case "usageEvent":
		return handleKiroUsage(chunk, state)
	default:
		return nil
	}
}

func kiroToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	if obj, ok := chunk["object"].(string); ok && obj == "chat.completion.chunk" {
		if _, cOk := chunk["choices"].([]any); cOk {
			return []map[string]any{chunk}
		}
	}

	meta := initKiroState(state)
	eventType := extractKiroEventType(chunk)

	return dispatchKiroEvent(eventType, chunk, meta, state)
}

func buildKiroChunk(meta map[string]any, delta, finishReason any) map[string]any {
	chunk := map[string]any{
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

	return chunk
}
