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

func commandCodeToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	if obj, ok := chunk["object"].(string); ok && obj == "chat.completion.chunk" {
		if _, ok := chunk["choices"].([]any); ok {
			return []map[string]any{chunk}
		}
	}

	event := chunk

	if raw, ok := chunk["raw"].(string); ok && raw != "" {
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
			event = parsed
		}
	}

	eventType, _ := event["type"].(string)
	if eventType == "" {
		return nil
	}

	ensureCCState(state, event)

	var out []map[string]any

	switch eventType {
	case "text-delta":
		text := ""
		if t, ok := event["text"].(string); ok {
			text = t
		} else if t, ok := event["delta"].(string); ok {
			text = t
		}

		if text == "" {
			break
		}

		delta := map[string]any{"content": text}
		if state.ChunkIndex == 0 {
			delta["role"] = schema.RoleAssistant
		}

		state.ChunkIndex++
		out = append(out, buildCCChunk(state, delta, nil))

	case "reasoning-delta":
		text, _ := event["text"].(string)
		if text == "" {
			break
		}

		delta := concerns.ReasoningDelta(text, state.ChunkIndex == 0)
		state.ChunkIndex++
		out = append(out, buildCCChunk(state, delta, nil))

	case "tool-input-start":
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
		if state.ToolIndexById == nil {
			state.ToolIndexById = make(map[string]int)
		}

		state.ToolIndexById[id] = idx
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
		out = append(out, buildCCChunk(state, delta, nil))

	case "tool-input-delta":
		id := ""
		if v, ok := event["id"].(string); ok {
			id = v
		} else if v, ok := event["toolCallId"].(string); ok {
			id = v
		}

		idx, exists := state.ToolIndexById[id]
		if !exists {
			break
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
		out = append(out, buildCCChunk(state, delta, nil))

	case "tool-call":
		toolCallId, _ := event["toolCallId"].(string)
		if state.ToolIndexById != nil {
			if _, exists := state.ToolIndexById[toolCallId]; exists {
				break
			}
		}

		idx := state.ToolIndex

		state.ToolIndex++
		if state.ToolIndexById == nil {
			state.ToolIndexById = make(map[string]int)
		}

		state.ToolIndexById[toolCallId] = idx

		argsStr := "{}"
		if input, ok := event["input"].(string); ok {
			argsStr = input
		} else if input, ok := event["input"].(map[string]any); ok {
			b, _ := json.Marshal(input)
			argsStr = string(b)
		}

		delta := map[string]any{
			"tool_calls": []any{map[string]any{
				"index": idx,
				"id":    toolCallId,
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
		out = append(out, buildCCChunk(state, delta, nil))

	case "finish-step":
		if reason, ok := event["finishReason"].(string); ok {
			state.FinishReason = concerns.ToOpenAIFinish(reason, "commandcode")
		}

		if usage, ok := event["usage"].(map[string]any); ok {
			state.RawUsage = usage
		}

	case "finish":
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

		out = append(out, finalChunk)

	case "error":
		errVal := event["error"]
		if errVal == nil {
			errVal = event["message"]
		}

		errStr := "unknown"
		if s, ok := errVal.(string); ok {
			errStr = s
		} else if b, ok := errVal.(map[string]any); ok {
			b2, _ := json.Marshal(b)
			errStr = string(b2)
		}

		out = append(out, buildCCChunk(state, map[string]any{"content": "\n\n[CommandCode error: " + errStr + "]"}, nil))
		out = append(out, buildCCChunk(state, map[string]any{}, "stop"))
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func ensureCCState(state *concerns.ResponseState, event map[string]any) {
	if state.ResponseCreated {
		return
	}

	state.ResponseCreated = true
	state.ResponseId = "chatcmpl-" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	state.Created = time.Now().Unix()

	if m, ok := event["model"].(string); ok && m != "" {
		state.Model = m
	}

	if state.Model == "" {
		state.Model = "commandcode"
	}

	state.ChunkIndex = 0
	state.ToolIndex = 0
	state.ToolIndexById = make(map[string]int)
}

func buildCCChunk(state *concerns.ResponseState, delta, finishReason any) map[string]any {
	return map[string]any{
		"object":  "chat.completion.chunk",
		"created": state.Created,
		"id":      state.ResponseId,
		"model":   state.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason,
		}},
	}
}
