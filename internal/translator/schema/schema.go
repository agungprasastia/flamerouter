package schema

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleDeveloper = "developer"
)

const (
	GeminiRoleUser  = "user"
	GeminiRoleModel = "model"
)

const (
	OpenaiBlockText       = "text"
	OpenaiBlockImageUrl   = "image_url"
	OpenaiBlockImage      = "image"
	OpenaiBlockInputAudio = "input_audio"
	OpenaiBlockAudioUrl   = "audio_url"
	OpenaiBlockFile       = "file"
	OpenaiBlockFunction   = "function"
)

const (
	ClaudeBlockText             = "text"
	ClaudeBlockImage            = "image"
	ClaudeBlockDocument         = "document"
	ClaudeBlockToolUse          = "tool_use"
	ClaudeBlockToolResult       = "tool_result"
	ClaudeBlockThinking         = "thinking"
	ClaudeBlockRedactedThinking = "redacted_thinking"
)

const (
	ResponsesItemMessage            = "message"
	ResponsesItemFunctionCall       = "function_call"
	ResponsesItemFunctionCallOutput = "function_call_output"
	ResponsesItemReasoning          = "reasoning"
	ResponsesItemOutputText         = "output_text"
	ResponsesItemInputText          = "input_text"
	ResponsesItemInputImage         = "input_image"
	ResponsesItemSummaryText        = "summary_text"
)

const (
	OpenaiFinishStop          = "stop"
	OpenaiFinishLength        = "length"
	OpenaiFinishToolCalls     = "tool_calls"
	OpenaiFinishContentFilter = "content_filter"
)

const (
	ClaudeStopEndTurn      = "end_turn"
	ClaudeStopMaxTokens    = "max_tokens"
	ClaudeStopToolUse      = "tool_use"
	ClaudeStopStopSequence = "stop_sequence"
)

const (
	GeminiFinishStop              = "STOP"
	GeminiFinishMaxTokens         = "MAX_TOKENS"
	GeminiFinishSafety            = "SAFETY"
	GeminiFinishRecitation        = "RECITATION"
	GeminiFinishBlocklist         = "BLOCKLIST"
	GeminiFinishProhibitedContent = "PROHIBITED_CONTENT"
)

var (
	ValidOpenaiContentTypes = map[string]bool{
		"text":        true,
		"image_url":   true,
		"image":       true,
		"input_audio": true,
		"audio_url":   true,
		"file":        true,
	}
	ValidOpenaiMessageTypes = map[string]bool{
		"text":        true,
		"image_url":   true,
		"image":       true,
		"tool_calls":  true,
		"tool_result": true,
	}
)

const (
	ModelFallback    = "unknown"
	DefaultImageMime = "image/png"
)
