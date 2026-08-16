package translator

const (
	AntigravityModelGemini3 = "gemini-3-flash-preview"
	AntigravityModelGemini2 = "gemini-2.5-flash"
	AntigravityModelGemini1 = "gemini-2.0-flash-001"
	AntigravityDefaultModel = "gemini-2.5-flash"
)

var ClaudeCodeSpoofHeaders = map[string]string{
	"User-Agent":         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"Accept":             "text/event-stream",
	"Accept-Language":    "en-US,en;q=0.9",
	"Cache-Control":      "no-cache",
	"Pragma":             "no-cache",
	"Sec-Ch-Ua":          `"Google Chrome";v="131", "Chromium";v="131", "Not_A Brand";v="24"`,
	"Sec-Ch-Ua-Mobile":   "?0",
	"Sec-Ch-Ua-Platform": `"macOS"`,
	"Sec-Fetch-Dest":     "empty",
	"Sec-Fetch-Mode":     "cors",
	"Sec-Fetch-Site":     "same-origin",
}

const (
	GeminiCodeMaxToolCount      = 1024
	GeminiCodeMaxThinkingTokens = 1048576
	GeminiCodeThinkingBudget    = 32768
)

const (
	CopilotDefaultModel      = "gpt-4o"
	CopilotRateLimitRequests = 10
	CopilotRateLimitWindow   = 60
	CopilotMaxTokens         = 16384
	CopilotThinkingModel     = "o3-mini"
	CopilotThinkingMaxTokens = 100000
)
