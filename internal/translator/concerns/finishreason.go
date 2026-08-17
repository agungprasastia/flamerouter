package concerns

import "flamerouter/internal/translator/schema"

func toOpenAIFinishClaude(reason string) string {
	switch reason {
	case schema.ClaudeStopEndTurn, schema.ClaudeStopStopSequence:
		return schema.OpenaiFinishStop
	case schema.ClaudeStopMaxTokens:
		return schema.OpenaiFinishLength
	case schema.ClaudeStopToolUse:
		return schema.OpenaiFinishToolCalls
	default:
		return schema.OpenaiFinishStop
	}
}

func toOpenAIFinishCommandCode(reason string) string {
	switch reason {
	case "stop", "error":
		return schema.OpenaiFinishStop
	case "length":
		return schema.OpenaiFinishLength
	case "tool-calls", "tool_use":
		return schema.OpenaiFinishToolCalls
	case "content-filter":
		return schema.OpenaiFinishContentFilter
	default:
		if reason == "" {
			return schema.OpenaiFinishStop
		}

		return reason
	}
}

func toOpenAIFinishGemini(reason string) string {
	switch reason {
	case schema.GeminiFinishStop:
		return schema.OpenaiFinishStop
	case schema.GeminiFinishMaxTokens:
		return schema.OpenaiFinishLength
	case schema.GeminiFinishSafety, schema.GeminiFinishRecitation,
		schema.GeminiFinishBlocklist, schema.GeminiFinishProhibitedContent:
		return schema.OpenaiFinishContentFilter
	default:
		return schema.OpenaiFinishStop
	}
}

func toOpenAIFinishKiro(reason string) string {
	switch reason {
	case "tool_calls", "tool_use":
		return schema.OpenaiFinishToolCalls
	case "length", "max_tokens":
		return schema.OpenaiFinishLength
	default:
		return schema.OpenaiFinishStop
	}
}

// ToOpenAIFinish maps provider-specific finish reasons to standard OpenAI finish reasons.
func ToOpenAIFinish(reason, format string) string {
	switch format {
	case "claude":
		return toOpenAIFinishClaude(reason)
	case "commandcode":
		return toOpenAIFinishCommandCode(reason)
	case "gemini":
		return toOpenAIFinishGemini(reason)
	case "kiro", "ollama":
		return toOpenAIFinishKiro(reason)
	default:
		if reason == "" {
			return schema.OpenaiFinishStop
		}

		return reason
	}
}

// FromOpenAIFinish maps standard OpenAI finish reasons to target format finish reasons.
func FromOpenAIFinish(reason, format string) string {
	switch format {
	case "claude":
		switch reason {
		case schema.OpenaiFinishStop:
			return schema.ClaudeStopEndTurn
		case schema.OpenaiFinishLength:
			return schema.ClaudeStopMaxTokens
		case schema.OpenaiFinishToolCalls:
			return schema.ClaudeStopToolUse
		default:
			return schema.ClaudeStopEndTurn
		}
	default:
		return reason
	}
}
