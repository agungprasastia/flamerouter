package usage

import (
	"encoding/json"
	"math"
	"strings"
)

// ExtractedUsage carries normalized prompt and completion tokens.
type ExtractedUsage struct {
	PromptTokens     int
	CompletionTokens int
	HasUsage         bool
}

// EstimateInputTokens estimates prompt tokens based on request body character length (~4 chars/token).
func EstimateInputTokens(body []byte) int {
	if len(body) == 0 {
		return 0
	}

	return int(math.Ceil(float64(len(body)) / 4.0))
}

// EstimateOutputTokens estimates completion tokens based on content length (~4 chars/token, min 1).
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

// ExtractUsageFromChunk inspects an SSE chunk object for token usage data (OpenAI, Claude, Gemini, Responses format).
func ExtractUsageFromChunk(chunk map[string]any) (ExtractedUsage, bool) {
	if chunk == nil {
		return ExtractedUsage{PromptTokens: 0, CompletionTokens: 0, HasUsage: false}, false
	}

	// 1. OpenAI / standard chunk.usage
	if u, ok := chunk["usage"].(map[string]any); ok && u != nil {
		prompt := getIntVal(u, "prompt_tokens")
		completion := getIntVal(u, "completion_tokens")

		if prompt > 0 || completion > 0 {
			return ExtractedUsage{PromptTokens: prompt, CompletionTokens: completion, HasUsage: true}, true
		}
	}

	// 2. Claude format (message_start / message_delta)
	if msg, ok := chunk["message"].(map[string]any); ok && msg != nil {
		if u, ok := msg["usage"].(map[string]any); ok && u != nil {
			prompt := getIntVal(u, "input_tokens")
			completion := getIntVal(u, "output_tokens")

			if prompt > 0 || completion > 0 {
				return ExtractedUsage{PromptTokens: prompt, CompletionTokens: completion, HasUsage: true}, true
			}
		}
	}

	// 3. OpenAI Responses API (response.completed / response.done)
	if resp, ok := chunk["response"].(map[string]any); ok && resp != nil {
		if u, ok := resp["usage"].(map[string]any); ok && u != nil {
			prompt := getIntVal(u, "input_tokens")
			if prompt == 0 {
				prompt = getIntVal(u, "prompt_tokens")
			}

			completion := getIntVal(u, "output_tokens")
			if completion == 0 {
				completion = getIntVal(u, "completion_tokens")
			}

			if prompt > 0 || completion > 0 {
				return ExtractedUsage{PromptTokens: prompt, CompletionTokens: completion, HasUsage: true}, true
			}
		}
	}

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

		if prompt > 0 || completion > 0 {
			return ExtractedUsage{PromptTokens: prompt, CompletionTokens: completion, HasUsage: true}, true
		}
	}

	return ExtractedUsage{PromptTokens: 0, CompletionTokens: 0, HasUsage: false}, false
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
