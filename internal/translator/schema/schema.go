// Package schema provides shared protocol constants and message types for protocol translation.
package schema

// Standard role constants.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleDeveloper = "developer"
)

// Gemini role constants.
const (
	GeminiRoleUser  = "user"
	GeminiRoleModel = "model"
)

// OpenAI content block type constants.
const (
	OpenaiBlockText       = "text"
	OpenaiBlockImageURL   = "image_url"
	OpenaiBlockImage      = "image"
	OpenaiBlockInputAudio = "input_audio"
	OpenaiBlockAudioURL   = "audio_url"
	OpenaiBlockFile       = "file"
	OpenaiBlockFunction   = "function"
)

// Claude content block type constants.
const (
	ClaudeBlockText             = "text"
	ClaudeBlockImage            = "image"
	ClaudeBlockDocument         = "document"
	ClaudeBlockToolUse          = "tool_use"
	ClaudeBlockToolResult       = "tool_result"
	ClaudeBlockThinking         = "thinking"
	ClaudeBlockRedactedThinking = "redacted_thinking"
)

// Responses API item type constants.
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

// OpenAI finish reason constants.
const (
	OpenaiFinishStop          = "stop"
	OpenaiFinishLength        = "length"
	OpenaiFinishToolCalls     = "tool_calls"
	OpenaiFinishContentFilter = "content_filter"
)

// Claude stop reason constants.
const (
	ClaudeStopEndTurn      = "end_turn"
	ClaudeStopMaxTokens    = "max_tokens"
	ClaudeStopToolUse      = "tool_use"
	ClaudeStopStopSequence = "stop_sequence"
)

// Gemini finish reason constants.
const (
	GeminiFinishStop              = "STOP"
	GeminiFinishMaxTokens         = "MAX_TOKENS"
	GeminiFinishSafety            = "SAFETY"
	GeminiFinishRecitation        = "RECITATION"
	GeminiFinishBlocklist         = "BLOCKLIST"
	GeminiFinishProhibitedContent = "PROHIBITED_CONTENT"
)

// ValidOpenaiContentTypes specifies valid content types in OpenAI message parts.
var ValidOpenaiContentTypes = map[string]bool{
	"text":        true,
	"image_url":   true,
	"image":       true,
	"input_audio": true,
	"audio_url":   true,
	"file":        true,
}

// ValidOpenaiMessageTypes specifies valid message types for OpenAI messages.
var ValidOpenaiMessageTypes = map[string]bool{
	"text":        true,
	"image_url":   true,
	"image":       true,
	"tool_calls":  true,
	"tool_result": true,
}

// Model fallback and default values.
const (
	ModelFallback    = "unknown"
	DefaultImageMime = "image/png"
)
