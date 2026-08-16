package concerns

import "flamerouter/internal/translator/schema"

func ToOpenAIFinish(reason, format string) string {
	switch format {
	case "claude":
		switch reason {
		case schema.ClaudeStopEndTurn:
			return schema.OpenaiFinishStop
		case schema.ClaudeStopMaxTokens:
			return schema.OpenaiFinishLength
		case schema.ClaudeStopToolUse:
			return schema.OpenaiFinishToolCalls
		case schema.ClaudeStopStopSequence:
			return schema.OpenaiFinishStop
		default:
			return schema.OpenaiFinishStop
		}
	case "commandcode":
		switch reason {
		case "stop":
			return schema.OpenaiFinishStop
		case "length":
			return schema.OpenaiFinishLength
		case "tool-calls", "tool_use":
			return schema.OpenaiFinishToolCalls
		case "content-filter":
			return schema.OpenaiFinishContentFilter
		case "error":
			return schema.OpenaiFinishStop
		default:
			if reason == "" {
				return schema.OpenaiFinishStop
			}

			return reason
		}
	case "gemini":
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
	case "kiro", "ollama":
		switch reason {
		case "tool_calls", "tool_use":
			return schema.OpenaiFinishToolCalls
		case "length", "max_tokens":
			return schema.OpenaiFinishLength
		default:
			return schema.OpenaiFinishStop
		}
	default:
		if reason == "" {
			return schema.OpenaiFinishStop
		}

		return reason
	}
}

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
