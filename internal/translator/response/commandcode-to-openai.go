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
	translator.Register(translator.FormatCommandCode, translator.FormatOpenAI, nil, commandCodeToOpenAIResponse)
}

func handleCCTextDelta(event map[string]any, state *concerns.ResponseState) []map[string]any {
	text := ""
	if t, ok := event["text"].(string); ok {
		text = t
	} else if t, ok := event["delta"].(string); ok {
		text = t
	}

	if text == "" {
		return nil
	}

	delta := map[string]any{"content": text}
	if state.ChunkIndex == 0 {
		delta["role"] = schema.RoleAssistant
	}

	state.ChunkIndex++

	return []map[string]any{buildCCChunk(state, delta, nil)}
}

func handleCCReasoningDelta(event map[string]any, state *concerns.ResponseState) []map[string]any {
	text, ok := event["text"].(string)
	if !ok || text == "" {
		return nil
	}

	delta := concerns.ReasoningDelta(text, state.ChunkIndex == 0)
	state.ChunkIndex++

	return []map[string]any{buildCCChunk(state, delta, nil)}
}

func handleCCToolInputStart(event map[string]any, state *concerns.ResponseState) []map[string]any {
	id := ""
	if v, ok := event["id"].(string); ok {
		id = v
	} else if v, ok := event["toolCallId"].(string); ok {
		id = v
	}

	if id == "" {
		id = "call_" + strconv.Itoa(state.ToolIndex)
	}

	idx := state.ToolIndex
	state.ToolIndex++

	if state.ToolIndexByID == nil {
		state.ToolIndexByID = make(map[string]int)
	}

	state.ToolIndexByID[id] = idx
	delta := map[string]any{
		"tool_calls": []any{map[string]any{
			"index": idx,
			"id":    id,
			"type":  schema.OpenaiBlockFunction,
			"function": map[string]any{
				"name":      event["toolName"],
				"arguments": "",
			},
		}},
	}

	if state.ChunkIndex == 0 {
		delta["role"] = schema.RoleAssistant
	}

	state.ChunkIndex++

	return []map[string]any{buildCCChunk(state, delta, nil)}
}

func handleCCToolInputDelta(event map[string]any, state *concerns.ResponseState) []map[string]any {
	id := ""
	if v, ok := event["id"].(string); ok {
		id = v
	} else if v, ok := event["toolCallId"].(string); ok {
		id = v
	}

	idx, exists := state.ToolIndexByID[id]
	if !exists {
		return nil
	}

	deltaText := ""
	if d, ok := event["delta"].(string); ok {
		deltaText = d
	} else if d, ok := event["inputTextDelta"].(string); ok {
		deltaText = d
	}

	delta := map[string]any{
		"tool_calls": []any{map[string]any{
			"index": idx,
			"function": map[string]any{
				"arguments": deltaText,
			},
		}},
	}

	return []map[string]any{buildCCChunk(state, delta, nil)}
}

func handleCCToolCall(event map[string]any, state *concerns.ResponseState) []map[string]any {
	toolCallID := ""
	if tid, ok := event["toolCallId"].(string); ok {
		toolCallID = tid
	}

	if state.ToolIndexByID != nil {
		if _, exists := state.ToolIndexByID[toolCallID]; exists {
			return nil
		}
	}

	idx := state.ToolIndex
	state.ToolIndex++

	if state.ToolIndexByID == nil {
		state.ToolIndexByID = make(map[string]int)
	}

	state.ToolIndexByID[toolCallID] = idx

	argsStr := "{}"
	if input, ok := event["input"].(string); ok {
		argsStr = input
	} else if input, ok := event["input"].(map[string]any); ok {
		b, err := json.Marshal(input)
		if err == nil {
			argsStr = string(b)
		}
	}

	delta := map[string]any{
		"tool_calls": []any{map[string]any{
			"index": idx,
			"id":    toolCallID,
			"type":  schema.OpenaiBlockFunction,
			"function": map[string]any{
				"name":      event["toolName"],
				"arguments": argsStr,
			},
		}},
	}
	if state.ChunkIndex == 0 {
		delta["role"] = schema.RoleAssistant
	}

	state.ChunkIndex++

	return []map[string]any{buildCCChunk(state, delta, nil)}
}

func handleCCFinishStep(event map[string]any, state *concerns.ResponseState) []map[string]any {
	if reason, ok := event["finishReason"].(string); ok {
		state.FinishReason = concerns.ToOpenAIFinish(reason, "commandcode")
	}

	if usage, ok := event["usage"].(map[string]any); ok {
		state.RawUsage = usage
	}

	return nil
}

func handleCCFinish(event map[string]any, state *concerns.ResponseState) []map[string]any {
	finishReason := state.FinishReason
	if finishReason == "" {
		if reason, ok := event["finishReason"].(string); ok {
			finishReason = concerns.ToOpenAIFinish(reason, "commandcode")
		} else {
			finishReason = "stop"
		}
	}

	finalChunk := buildCCChunk(state, map[string]any{}, finishReason)

	totalUsage := event["totalUsage"]
	if totalUsage == nil {
		totalUsage = state.RawUsage
	}

	if tu, ok := totalUsage.(map[string]any); ok {
		usage := concerns.ToOpenAIUsage(tu, "commandcode")
		if usage != nil {
			finalChunk["usage"] = usage
		}
	}

	return []map[string]any{finalChunk}
}

func handleCCError(event map[string]any, state *concerns.ResponseState) []map[string]any {
	errVal := event["error"]
	if errVal == nil {
		errVal = event["message"]
	}

	errStr := "unknown"
	if s, ok := errVal.(string); ok {
		errStr = s
	} else if b, ok := errVal.(map[string]any); ok {
		b2, err := json.Marshal(b)
		if err == nil {
			errStr = string(b2)
		}
	}

	return []map[string]any{
		buildCCChunk(state, map[string]any{"content": "\n\n[CommandCode error: " + errStr + "]"}, nil),
		buildCCChunk(state, map[string]any{}, "stop"),
	}
}

func parseCCRawEvent(chunk map[string]any) map[string]any {
	raw, ok := chunk["raw"].(string)
	if !ok || raw == "" {
		return chunk
	}

	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[DONE]" {
		return nil
	}

	jsonStr := raw
	if strings.HasPrefix(jsonStr, "data:") {
		jsonStr = strings.TrimSpace(jsonStr[5:])
	}

	var parsed map[string]any
	if json.Unmarshal([]byte(jsonStr), &parsed) == nil {
		return parsed
	}

	return chunk
}

func dispatchCCEvent(eventType string, event map[string]any, state *concerns.ResponseState) []map[string]any {
	switch eventType {
	case "text-delta":
		return handleCCTextDelta(event, state)
	case "reasoning-delta":
		return handleCCReasoningDelta(event, state)
	case "tool-input-start":
		return handleCCToolInputStart(event, state)
	case "tool-input-delta":
		return handleCCToolInputDelta(event, state)
	case "tool-call":
		return handleCCToolCall(event, state)
	case "finish-step":
		return handleCCFinishStep(event, state)
	case "finish":
		return handleCCFinish(event, state)
	case "error":
		return handleCCError(event, state)
	default:
		return nil
	}
}

func commandCodeToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	if obj, ok := chunk["object"].(string); ok && obj == "chat.completion.chunk" {
		if _, ok := chunk["choices"].([]any); ok {
			return []map[string]any{chunk}
		}
	}

	event := parseCCRawEvent(chunk)
	if event == nil {
		return nil
	}

	eventType, ok := event["type"].(string)
	if !ok || eventType == "" {
		return nil
	}

	ensureCCState(state, event)

	return dispatchCCEvent(eventType, event, state)
}

func ensureCCState(state *concerns.ResponseState, event map[string]any) {
	if state.ResponseCreated {
		return
	}

	state.ResponseCreated = true
	state.ResponseID = "chatcmpl-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	state.Created = time.Now().Unix()

	if m, ok := event["model"].(string); ok && m != "" {
		state.Model = m
	}

	if state.Model == "" {
		state.Model = "commandcode"
	}

	state.ChunkIndex = 0
	state.ToolIndex = 0
	state.ToolIndexByID = make(map[string]int)
}

func buildCCChunk(state *concerns.ResponseState, delta, finishReason any) map[string]any {
	return map[string]any{
		"object":  "chat.completion.chunk",
		"created": state.Created,
		"id":      state.ResponseID,
		"model":   state.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason,
		}},
	}
}
