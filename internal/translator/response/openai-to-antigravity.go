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

func updateAntigravityToolFunction(accum, fn map[string]any) {
	if name, nOk := fn["name"].(string); nOk {
		prevName := ""
		if pn, pnOk := accum["name"].(string); pnOk {
			prevName = pn
		}

		accum["name"] = prevName + name
	}

	if args, aOk := fn["arguments"].(string); aOk {
		prevArgs := ""
		if pa, paOk := accum["arguments"].(string); paOk {
			prevArgs = pa
		}

		accum["arguments"] = prevArgs + args
	}
}

func accumulateAntigravityToolCalls(tc []any, state *concerns.ResponseState) {
	for _, tcRaw := range tc {
		toolCall, ok := tcRaw.(map[string]any)
		if !ok {
			continue
		}

		idx := 0
		if i, iOk := toolCall["index"].(float64); iOk {
			idx = int(i)
		}

		if _, exists := state.ToolCallAccum[idx]; !exists {
			state.ToolCallAccum[idx] = map[string]any{"id": "", "name": "", "arguments": ""}
		}

		accum := state.ToolCallAccum[idx]
		if id, idOk := toolCall["id"].(string); idOk && id != "" {
			accum["id"] = id
		}

		if fn, fnOk := toolCall["function"].(map[string]any); fnOk {
			updateAntigravityToolFunction(accum, fn)
		}
	}
}

func finalizeAntigravityToolCalls(state *concerns.ResponseState) []any {
	parts := make([]any, 0, len(state.ToolCallAccum))

	for _, accum := range state.ToolCallAccum {
		var args map[string]any

		if argsStr, ok := accum["arguments"].(string); ok && argsStr != "" {
			if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
				args = make(map[string]any)
			}
		}

		name := ""
		if n, ok := accum["name"].(string); ok {
			name = n
		}

		parts = append(parts, map[string]any{
			"functionCall": map[string]any{
				"name": name,
				"args": args,
			},
		})
	}

	return parts
}

func extractAntigravityParts(delta map[string]any, finishReason string, state *concerns.ResponseState) []any {
	var parts []any

	if delta != nil {
		if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
			parts = append(parts, map[string]any{"thought": true, "text": reasoning})
		}

		if content, ok := delta["content"].(string); ok && content != "" {
			parts = append(parts, map[string]any{"text": content})
		}

		if tc, ok := delta["tool_calls"].([]any); ok {
			accumulateAntigravityToolCalls(tc, state)
		}
	}

	if finishReason != "" {
		parts = append(parts, finalizeAntigravityToolCalls(state)...)
	}

	if len(parts) == 0 && finishReason != "" {
		parts = append(parts, map[string]any{"text": ""})
	}

	return parts
}

func mapAntigravityFinishReason(finishReason string) string {
	reasonMap := map[string]string{
		"stop":           "STOP",
		"length":         "MAX_TOKENS",
		"tool_calls":     "STOP",
		"content_filter": "SAFETY",
	}

	if mapped, ok := reasonMap[finishReason]; ok {
		return mapped
	}

	return "STOP"
}

func buildAntigravityResponse(state *concerns.ResponseState, parts []any, finishReason string, chunkUsage map[string]any) map[string]any {
	candidate := map[string]any{
		"content": map[string]any{
			"role":  schema.GeminiRoleModel,
			"parts": parts,
		},
	}

	if finishReason != "" {
		candidate["finishReason"] = mapAntigravityFinishReason(finishReason)
	}

	response := map[string]any{
		"candidates":   []any{candidate},
		"modelVersion": state.Model,
		"responseId":   state.ResponseID,
	}

	if chunkUsage != nil {
		if usage := parseAntigravityUsage(chunkUsage); usage != nil {
			response["usageMetadata"] = usage
		}
	}

	return response
}

func initAntigravityState(chunk map[string]any, state *concerns.ResponseState) {
	if state.ToolCallAccum == nil {
		state.ToolCallAccum = make(map[int]map[string]any)
	}

	if state.ResponseID == "" {
		state.ResponseID = "resp_" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	if state.Model == "" {
		if m, mOk := chunk["model"].(string); mOk {
			state.Model = m
		}
	}
}

func openaiToAntigravityResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	choice, ok := extractFirstChoice(chunk)
	if !ok || choice == nil {
		if chunkUsage, uOk := chunk["usage"].(map[string]any); uOk {
			state.RawUsage = parseAntigravityUsage(chunkUsage)
		}

		return nil
	}

	var delta map[string]any
	if d, dOk := choice["delta"].(map[string]any); dOk {
		delta = d
	}

	finishReason := ""
	if fr, frOk := choice["finish_reason"].(string); frOk {
		finishReason = fr
	}

	initAntigravityState(chunk, state)

	parts := extractAntigravityParts(delta, finishReason, state)
	if len(parts) == 0 {
		return nil
	}

	var chunkUsage map[string]any
	if u, uOk := chunk["usage"].(map[string]any); uOk {
		chunkUsage = u
	}

	return []map[string]any{{"response": buildAntigravityResponse(state, parts, finishReason, chunkUsage)}}
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
