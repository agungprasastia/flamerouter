package usage

import (
	"encoding/json"
	"math"
	"strings"
)

// ExtractedUsage carries token counts parsed from a provider SSE chunk.
type ExtractedUsage struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	HasUsage         bool
}

var noUsage = ExtractedUsage{PromptTokens: 0, CompletionTokens: 0, CachedTokens: 0, HasUsage: false}

// EstimateInputTokens guesses prompt tokens from request body size (~4 chars/token).
func EstimateInputTokens(body []byte) int {
	if len(body) == 0 {
		return 0
	}

	return int(math.Ceil(float64(len(body)) / 4.0))
}

// EstimateOutputTokens guesses completion tokens from content length (~4 chars/token, min 1).
func EstimateOutputTokens(contentLength int) int {
	if contentLength <= 0 {
		return 0
	}

	tokens := int(math.Floor(float64(contentLength) / 4.0))
	if tokens < 1 {
		tokens = 1
	}

	return tokens
}

func extractOpenAIUsage(chunk map[string]any) (ExtractedUsage, bool) {
	u, ok := chunk["usage"].(map[string]any)
	if !ok || u == nil {
		return noUsage, false
	}

	prompt := getIntVal(u, "prompt_tokens")
	completion := getIntVal(u, "completion_tokens")

	cached := getIntVal(u, "cached_tokens")
	if cached == 0 {
		if details, okD := u["prompt_tokens_details"].(map[string]any); okD {
			cached = getIntVal(details, "cached_tokens")
		}
	}

	if cached == 0 {
		cached = getIntVal(u, "prompt_cache_hit_tokens")
	}

	if cached == 0 {
		cached = getIntVal(u, "cache_read_input_tokens")
	}

	if prompt > 0 || completion > 0 {
		return ExtractedUsage{PromptTokens: prompt, CompletionTokens: completion, CachedTokens: cached, HasUsage: true}, true
	}

	return noUsage, false
}

func extractClaudeUsage(chunk map[string]any) (ExtractedUsage, bool) {
	msg, ok := chunk["message"].(map[string]any)
	if !ok || msg == nil {
		return noUsage, false
	}

	u, ok := msg["usage"].(map[string]any)
	if !ok || u == nil {
		return noUsage, false
	}

	prompt := getIntVal(u, "input_tokens")
	completion := getIntVal(u, "output_tokens")
	cached := getIntVal(u, "cache_read_input_tokens")
	cacheCreation := getIntVal(u, "cache_creation_input_tokens")
	totalPrompt := prompt + cached + cacheCreation

	if totalPrompt > 0 || completion > 0 {
		return ExtractedUsage{PromptTokens: totalPrompt, CompletionTokens: completion, CachedTokens: cached, HasUsage: true}, true
	}

	return noUsage, false
}

func extractResponsesUsage(chunk map[string]any) (ExtractedUsage, bool) {
	resp, ok := chunk["response"].(map[string]any)
	if !ok || resp == nil {
		return noUsage, false
	}

	u, ok := resp["usage"].(map[string]any)
	if !ok || u == nil {
		return noUsage, false
	}

	prompt := getIntVal(u, "input_tokens")
	if prompt == 0 {
		prompt = getIntVal(u, "prompt_tokens")
	}

	completion := getIntVal(u, "output_tokens")
	if completion == 0 {
		completion = getIntVal(u, "completion_tokens")
	}

	cached := 0
	if details, okD := u["input_tokens_details"].(map[string]any); okD {
		cached = getIntVal(details, "cached_tokens")
	}

	if prompt > 0 || completion > 0 {
		return ExtractedUsage{PromptTokens: prompt, CompletionTokens: completion, CachedTokens: cached, HasUsage: true}, true
	}

	return noUsage, false
}

func extractGeminiUsage(chunk map[string]any) (ExtractedUsage, bool) {
	usageMeta, ok := chunk["usageMetadata"].(map[string]any)
	if !ok {
		if r, ok := chunk["response"].(map[string]any); ok && r != nil {
			if um, okMap := r["usageMetadata"].(map[string]any); okMap {
				usageMeta = um
			} else if nestedResp, okNested := r["response"].(map[string]any); okNested && nestedResp != nil {
				if umNested, okNestedMap := nestedResp["usageMetadata"].(map[string]any); okNestedMap {
					usageMeta = umNested
				}
			}
		}
	}

	if usageMeta != nil {
		prompt := getIntVal(usageMeta, "promptTokenCount")
		completion := getIntVal(usageMeta, "candidatesTokenCount")
		cached := getIntVal(usageMeta, "cachedContentTokenCount")

		if prompt > 0 || completion > 0 {
			return ExtractedUsage{PromptTokens: prompt, CompletionTokens: completion, CachedTokens: cached, HasUsage: true}, true
		}
	}

	return noUsage, false
}

// ExtractUsageFromChunk inspects an SSE chunk object for token usage data (OpenAI, Claude, Gemini, Responses format).
func ExtractUsageFromChunk(chunk map[string]any) (ExtractedUsage, bool) {
	if chunk == nil {
		return ExtractedUsage{PromptTokens: 0, CompletionTokens: 0, CachedTokens: 0, HasUsage: false}, false
	}

	if u, ok := extractOpenAIUsage(chunk); ok {
		return u, true
	}

	if u, ok := extractClaudeUsage(chunk); ok {
		return u, true
	}

	if u, ok := extractResponsesUsage(chunk); ok {
		return u, true
	}

	if u, ok := extractGeminiUsage(chunk); ok {
		return u, true
	}

	return ExtractedUsage{PromptTokens: 0, CompletionTokens: 0, CachedTokens: 0, HasUsage: false}, false
}

// ExtractContentLengthFromChunk calculates the character length of content delta in a chunk.
func ExtractContentLengthFromChunk(chunk map[string]any) int {
	if chunk == nil {
		return 0
	}

	total := 0

	// Choices delta (OpenAI)
	if choices, ok := chunk["choices"].([]any); ok {
		for _, c := range choices {
			if choiceMap, ok := c.(map[string]any); ok {
				if delta, ok := choiceMap["delta"].(map[string]any); ok {
					if content, ok := delta["content"].(string); ok {
						total += len(content)
					}

					if reasoning, ok := delta["reasoning_content"].(string); ok {
						total += len(reasoning)
					}
				}

				if text, ok := choiceMap["text"].(string); ok {
					total += len(text)
				}
			}
		}
	}

	// Claude content_block_delta
	if delta, ok := chunk["delta"].(map[string]any); ok {
		if text, ok := delta["text"].(string); ok {
			total += len(text)
		}

		if thinking, ok := delta["thinking"].(string); ok {
			total += len(thinking)
		}
	}

	// Gemini candidates
	if candidates, ok := chunk["candidates"].([]any); ok {
		for _, c := range candidates {
			if candMap, ok := c.(map[string]any); ok {
				if content, ok := candMap["content"].(map[string]any); ok {
					if parts, ok := content["parts"].([]any); ok {
						for _, p := range parts {
							if partMap, ok := p.(map[string]any); ok {
								if text, ok := partMap["text"].(string); ok {
									total += len(text)
								}
							}
						}
					}
				}
			}
		}
	}

	return total
}

func getIntVal(m map[string]any, key string) int {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case json.Number:
			if i, err := v.Int64(); err == nil {
				return int(i)
			}
		}
	}

	return 0
}

// ParseSSELinePayload extracts JSON bytes from a "data: ..." SSE line.
func ParseSSELinePayload(line string) ([]byte, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return nil, false
	}

	data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if data == "" || data == "[DONE]" {
		return nil, false
	}

	return []byte(data), true
}
