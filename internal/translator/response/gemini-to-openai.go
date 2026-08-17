package response

import (
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"time"
)

func init() {
	translator.Register(translator.FormatGemini, translator.FormatOpenAI, nil, geminiToOpenAIResponse)
	translator.Register(translator.FormatAntigravity, translator.FormatOpenAI, nil, geminiToOpenAIResponse)
	translator.Register(translator.FormatGeminiCLI, translator.FormatOpenAI, nil, geminiToOpenAIResponse)
	translator.Register(translator.FormatVertex, translator.FormatOpenAI, nil, geminiToOpenAIResponse)
}

func parseGeminiPart(p map[string]any, state *concerns.ResponseState) map[string]any {
	if text, ok := p["text"].(string); ok && text != "" {
		if thought, tOk := p["thought"].(bool); tOk && thought {
			return buildGeminiOpenAIChunk(state, map[string]any{"content": "<think>" + text + "</think>"}, nil)
		}

		return buildGeminiOpenAIChunk(state, map[string]any{"content": text}, nil)
	}

	if fc, ok := p["functionCall"].(map[string]any); ok {
		name := ""
		if n, nOk := fc["name"].(string); nOk {
			name = n
		}

		args := fc["args"]

		argsBytes, err := jsonMarshal(args)
		if err != nil {
			argsBytes = []byte("{}")
		}

		return buildGeminiOpenAIChunk(state, map[string]any{
			"tool_calls": []any{map[string]any{
				"index": 0,
				"id":    "call_" + name,
				"type":  "function",
				"function": map[string]any{
					"name":      name,
					"arguments": string(argsBytes),
				},
			}},
		}, nil)
	}

	return nil
}

func parseGeminiParts(parts []any, state *concerns.ResponseState) []map[string]any {
	var results []map[string]any

	for _, pRaw := range parts {
		p, ok := pRaw.(map[string]any)
		if !ok || p == nil {
			continue
		}

		if chunk := parseGeminiPart(p, state); chunk != nil {
			results = append(results, chunk)
		}
	}

	return results
}

func parseGeminiUsage(usage map[string]any, state *concerns.ResponseState) {
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

	if pt, ok := usage["promptTokenCount"].(float64); ok {
		state.Usage.PromptTokens = int(pt)
		state.Usage.InputTokens = int(pt)
	}

	if ct, ok := usage["candidatesTokenCount"].(float64); ok {
		state.Usage.CompletionTokens = int(ct)
		state.Usage.OutputTokens = int(ct)
		state.Usage.TotalTokens = state.Usage.PromptTokens + int(ct)
	}
}

func handleGeminiStart(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if state.MessageStartSent {
		return nil
	}

	state.MessageStartSent = true
	state.MessageID = "msg_" + time.Now().Format("20060102150405.000")

	if id, ok := chunk["responseId"].(string); ok && id != "" {
		state.MessageID = id
	}

	state.Model = "gemini"

	return []map[string]any{buildGeminiOpenAIChunk(state, map[string]any{"role": "assistant"}, nil)}
}

func extractGeminiCandidate(chunk map[string]any) map[string]any {
	candidates, ok := chunk["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		return nil
	}

	candidate, ok := candidates[0].(map[string]any)
	if !ok {
		return nil
	}

	return candidate
}

func parseCandidateContent(candidate map[string]any, state *concerns.ResponseState) []map[string]any {
	content, cOk := candidate["content"].(map[string]any)
	if !cOk || content == nil {
		return nil
	}

	parts, pOk := content["parts"].([]any)
	if !pOk {
		return nil
	}

	return parseGeminiParts(parts, state)
}

func geminiToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	candidate := extractGeminiCandidate(chunk)
	if candidate == nil {
		return nil
	}

	results := handleGeminiStart(chunk, state)
	results = append(results, parseCandidateContent(candidate, state)...)

	if finishReason, fOk := candidate["finishReason"].(string); fOk && finishReason != "" {
		fr := convertGeminiFinish(finishReason)
		results = append(results, buildGeminiOpenAIChunk(state, map[string]any{}, &fr))
	}

	if usage, uOk := chunk["usageMetadata"].(map[string]any); uOk {
		parseGeminiUsage(usage, state)
	}

	if len(results) == 0 {
		return nil
	}

	return results
}

func buildGeminiOpenAIChunk(state *concerns.ResponseState, delta map[string]any, finishReason *string) map[string]any {
	chunk := map[string]any{
		"id":      state.MessageID,
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

func convertGeminiFinish(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY":
		return "content_filter"
	case "RECITATION":
		return "content_filter"
	default:
		return "stop"
	}
}

func jsonMarshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}"), err
	}

	return b, nil
}
