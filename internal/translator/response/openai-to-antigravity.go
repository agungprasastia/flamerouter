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
	translator.Register(translator.FormatOpenAI, translator.FormatAntigravity, nil, openaiToAntigravityResponse)
}

func openaiToAntigravityResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	choice, _ := extractFirstChoice(chunk)
	if choice == nil {
		if chunkUsage, ok := chunk["usage"].(map[string]any); ok {
			state.RawUsage = parseAntigravityUsage(chunkUsage)
		}

		return nil
	}

	delta, _ := choice["delta"].(map[string]any)
	finishReason, _ := choice["finish_reason"].(string)

	if state.ToolCallAccum == nil {
		state.ToolCallAccum = make(map[int]map[string]any)
	}

	if state.ResponseId == "" {
		state.ResponseId = "resp_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	if state.Model == "" {
		state.Model, _ = chunk["model"].(string)
	}

	var parts []any

	if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
		parts = append(parts, map[string]any{"thought": true, "text": reasoning})
	}

	if content, ok := delta["content"].(string); ok && content != "" {
		parts = append(parts, map[string]any{"text": content})
	}

	if tc, ok := delta["tool_calls"].([]any); ok {
		for _, tcRaw := range tc {
			toolCall, ok := tcRaw.(map[string]any)
			if !ok {
				continue
			}

			idx := 0
			if i, ok := toolCall["index"].(float64); ok {
				idx = int(i)
			}

			if _, exists := state.ToolCallAccum[idx]; !exists {
				state.ToolCallAccum[idx] = map[string]any{"id": "", "name": "", "arguments": ""}
			}

			accum := state.ToolCallAccum[idx]
			if id, ok := toolCall["id"].(string); ok && id != "" {
				accum["id"] = id
			}

			if fn, ok := toolCall["function"].(map[string]any); ok {
				if name, ok := fn["name"].(string); ok {
					accum["name"] = accum["name"].(string) + name
				}

				if args, ok := fn["arguments"].(string); ok {
					accum["arguments"] = accum["arguments"].(string) + args
				}
			}
		}

		if len(parts) == 0 && finishReason == "" {
			return nil
		}
	}

	if finishReason != "" {
		for idx, accum := range state.ToolCallAccum {
			var args map[string]any

			argsStr, _ := accum["arguments"].(string)
			if argsStr != "" {
				json.Unmarshal([]byte(argsStr), &args)
			}

			name, _ := accum["name"].(string)
			parts = append(parts, map[string]any{
				"functionCall": map[string]any{
					"name": name,
					"args": args,
				},
			})
			_ = idx
		}
	}

	if len(parts) == 0 && finishReason == "" {
		return nil
	}

	if len(parts) == 0 && finishReason != "" {
		parts = append(parts, map[string]any{"text": ""})
	}

	candidate := map[string]any{
		"content": map[string]any{
			"role":  schema.GeminiRoleModel,
			"parts": parts,
		},
	}

	if finishReason != "" {
		reasonMap := map[string]string{
			"stop":           "STOP",
			"length":         "MAX_TOKENS",
			"tool_calls":     "STOP",
			"content_filter": "SAFETY",
		}
		if mapped, ok := reasonMap[finishReason]; ok {
			candidate["finishReason"] = mapped
		} else {
			candidate["finishReason"] = "STOP"
		}
	}

	response := map[string]any{
		"candidates":   []any{candidate},
		"modelVersion": state.Model,
		"responseId":   state.ResponseId,
	}

	if chunkUsage, ok := chunk["usage"].(map[string]any); ok {
		usage := parseAntigravityUsage(chunkUsage)
		if usage != nil {
			response["usageMetadata"] = usage
		}
	}

	return []map[string]any{{"response": response}}
}

func parseAntigravityUsage(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}

	result := map[string]any{
		"promptTokenCount":     getInt(raw, "prompt_tokens"),
		"candidatesTokenCount": getInt(raw, "completion_tokens"),
		"totalTokenCount":      getInt(raw, "total_tokens"),
	}

	if details, ok := raw["completion_tokens_details"].(map[string]any); ok {
		if rt, ok := details["reasoning_tokens"].(float64); ok && rt > 0 {
			result["thoughtsTokenCount"] = int(rt)
		}
	}

	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		if ct, ok := details["cached_tokens"].(float64); ok && ct > 0 {
			result["cachedContentTokenCount"] = int(ct)
		}
	}

	return result
}

func extractFirstChoice(chunk map[string]any) (map[string]any, bool) {
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, false
	}

	choice, ok := choices[0].(map[string]any)

	return choice, ok
}
