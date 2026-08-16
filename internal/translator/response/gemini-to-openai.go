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

func geminiToOpenAIResponse(chunk map[string]any, state *concerns.ResponseState) []map[string]any {
	if chunk == nil {
		return nil
	}

	var results []map[string]any

	candidates, ok := chunk["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		return nil
	}

	candidate, _ := candidates[0].(map[string]any)
	if candidate == nil {
		return nil
	}

	if !state.MessageStartSent {
		state.MessageStartSent = true
		state.MessageID = "msg_" + time.Now().Format("20060102150405.000")

		if id, ok := chunk["responseId"].(string); ok && id != "" {
			state.MessageID = id
		}

		state.Model = "gemini"
		results = append(results, buildGeminiOpenAIChunk(state, map[string]any{"role": "assistant"}, nil))
	}

	content, _ := candidate["content"].(map[string]any)
	if content != nil {
		parts, _ := content["parts"].([]any)
		for _, pRaw := range parts {
			p, _ := pRaw.(map[string]any)
			if p == nil {
				continue
			}

			if text, ok := p["text"].(string); ok && text != "" {
				results = append(results, buildGeminiOpenAIChunk(state, map[string]any{"content": text}, nil))
			}

			if fc, ok := p["functionCall"].(map[string]any); ok {
				name, _ := fc["name"].(string)
				args := fc["args"]
				argsBytes, _ := jsonMarshal(args)
				results = append(results, buildGeminiOpenAIChunk(state, map[string]any{
					"tool_calls": []any{map[string]any{
						"index": 0,
						"id":    "call_" + name,
						"type":  "function",
						"function": map[string]any{
							"name":      name,
							"arguments": string(argsBytes),
						},
					}},
				}, nil))
			}

			if thought, ok := p["thought"].(bool); ok && thought {
				if text, ok := p["text"].(string); ok && text != "" {
					results = append(results, buildGeminiOpenAIChunk(state, map[string]any{"content": "<think>" + text + "</think>"}, nil))
				}
			}
		}
	}

	finishReason, _ := candidate["finishReason"].(string)
	if finishReason != "" {
		fr := convertGeminiFinish(finishReason)
		results = append(results, buildGeminiOpenAIChunk(state, map[string]any{}, &fr))
	}

	if usage, ok := chunk["usageMetadata"].(map[string]any); ok {
		if state.Usage == nil {
			state.Usage = &concerns.UsageInfo{}
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
