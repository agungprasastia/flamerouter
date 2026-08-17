package concerns

// BuildUsage builds OpenAI usage object. Details only when > 0.
func BuildUsage(promptTokens, completionTokens, totalTokens, cachedTokens, cacheCreationTokens, reasoningTokens int) map[string]any {
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}

	usage := map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": completionTokens,
		"total_tokens":      totalTokens,
	}

	if cachedTokens > 0 || cacheCreationTokens > 0 {
		details := map[string]any{}
		if cachedTokens > 0 {
			details["cached_tokens"] = cachedTokens
		}

		if cacheCreationTokens > 0 {
			details["cache_creation_tokens"] = cacheCreationTokens
		}

		usage["prompt_tokens_details"] = details
	}

	if reasoningTokens > 0 {
		usage["completion_tokens_details"] = map[string]any{
			"reasoning_tokens": reasoningTokens,
		}
	}

	return usage
}

func nInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func parseClaudeUsage(raw map[string]any) map[string]any {
	input := nInt(raw["input_tokens"])
	output := nInt(raw["output_tokens"])
	cacheRead := nInt(raw["cache_read_input_tokens"])
	cacheCreate := nInt(raw["cache_creation_input_tokens"])
	prompt := input + cacheRead + cacheCreate

	return BuildUsage(prompt, output, prompt+output, cacheRead, cacheCreate, 0)
}

func parseGeminiUsage(raw map[string]any) map[string]any {
	cached := nInt(raw["cachedContentTokenCount"])
	prompt := nInt(raw["promptTokenCount"])
	thoughts := nInt(raw["thoughtsTokenCount"])
	total := nInt(raw["totalTokenCount"])
	candidates := nInt(raw["candidatesTokenCount"])

	if candidates == 0 && total > 0 {
		candidates = total - prompt - thoughts
		if candidates < 0 {
			candidates = 0
		}
	}

	return BuildUsage(prompt, candidates+thoughts, total, cached, 0, thoughts)
}

func parseKiroUsage(raw map[string]any) map[string]any {
	input := nInt(raw["inputTokens"])
	if input == 0 {
		input = nInt(raw["prompt_tokens"])
	}

	output := nInt(raw["outputTokens"])
	if output == 0 {
		output = nInt(raw["completion_tokens"])
	}

	cached := nInt(raw["cache_read_input_tokens"])
	if cached == 0 {
		cached = nInt(raw["cachedTokens"])
	}

	if cached == 0 {
		cached = nInt(raw["cached_tokens"])
	}

	cacheCreate := nInt(raw["cache_creation_input_tokens"])

	return BuildUsage(input, output, input+output, cached, cacheCreate, 0)
}

func parseCommandCodeUsage(raw map[string]any) map[string]any {
	input := nInt(raw["inputTokens"])
	if input == 0 {
		input = nInt(raw["prompt_tokens"])
	}

	output := nInt(raw["outputTokens"])
	if output == 0 {
		output = nInt(raw["completion_tokens"])
	}

	total := nInt(raw["totalTokens"])
	if total == 0 {
		total = input + output
	}

	return BuildUsage(input, output, total, 0, 0, 0)
}

// ToOpenAIUsage converts provider-native usage → OpenAI usage.
func ToOpenAIUsage(raw map[string]any, kind string) map[string]any {
	if raw == nil {
		return nil
	}

	switch kind {
	case "claude":
		return parseClaudeUsage(raw)
	case "gemini":
		return parseGeminiUsage(raw)
	case "kiro":
		return parseKiroUsage(raw)
	case "ollama":
		input := nInt(raw["prompt_eval_count"])
		output := nInt(raw["eval_count"])

		return BuildUsage(input, output, input+output, 0, 0, 0)
	case "commandcode":
		return parseCommandCodeUsage(raw)
	default:
		return raw
	}
}
