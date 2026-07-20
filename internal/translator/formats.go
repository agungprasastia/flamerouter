package translator

const (
	FormatOpenAI           = "openai"
	FormatOpenAIResponses  = "openai-responses"
	FormatOpenAIResponse   = "openai-response"
	FormatClaude           = "claude"
	FormatGemini           = "gemini"
	FormatGeminiCLI        = "gemini-cli"
	FormatVertex           = "vertex"
	FormatCodex            = "codex"
	FormatAntigravity      = "antigravity"
	FormatKiro             = "kiro"
	FormatCursor           = "cursor"
	FormatOllama           = "ollama"
	FormatCommandCode      = "commandcode"
	FormatResponses        = "responses"
)

func DetectFormatByEndpoint(pathname string, body map[string]any) string {
	if contains(pathname, "/v1/responses") {
		return FormatOpenAIResponses
	}
	if contains(pathname, "/v1/messages") {
		return FormatClaude
	}
	if contains(pathname, "/v1/chat/completions") {
		if _, ok := body["input"]; ok {
			if _, isArray := body["input"].([]any); isArray {
				return FormatOpenAI
			}
		}
	}
	return ""
}

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

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstr(s, sub))
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
