// Package translator provides core translator registries, types, and format detection logic.
package translator

import "strings"

// Supported translator format identifiers.
const (
	FormatOpenAI          = "openai"
	FormatOpenAIResponses = "openai-responses"
	FormatOpenAIResponse  = "openai-response"
	FormatClaude          = "claude"
	FormatGemini          = "gemini"
	FormatGeminiCLI       = "gemini-cli"
	FormatVertex          = "vertex"
	FormatCodex           = "codex"
	FormatAntigravity     = "antigravity"
	FormatKiro            = "kiro"
	FormatCursor          = "cursor"
	FormatOllama          = "ollama"
	FormatCommandCode     = "commandcode"
	FormatResponses       = "responses"
)

// DetectFormatByEndpoint detects the API format based on request pathname and body.
func DetectFormatByEndpoint(pathname string, body map[string]any) string {
	if strings.Contains(pathname, "/v1/responses") {
		return FormatOpenAIResponses
	}

	if strings.Contains(pathname, "/v1/messages") {
		return FormatClaude
	}

	if strings.Contains(pathname, "/v1/chat/completions") {
		if _, ok := body["input"]; ok {
			if _, isArray := body["input"].([]any); isArray {
				return FormatOpenAI
			}
		}
	}

	return ""
}

// DetectSourceFormat inspects the request body payload to determine its source format.
func DetectSourceFormat(body map[string]any) string {
	if _, ok := body["thinking"]; ok {
		return FormatClaude
	}

	if _, ok := body["system"]; ok {
		return FormatClaude
	}

	if _, ok := body["contents"]; ok {
		return FormatGemini
	}

	if _, ok := body["generationConfig"]; ok {
		return FormatGemini
	}

	if _, ok := body["input"]; ok {
		return FormatOpenAIResponses
	}

	return FormatOpenAI
}
