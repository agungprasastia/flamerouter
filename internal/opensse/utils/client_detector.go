package utils

import (
	"net/http"
	"strings"
)

var knownUASubstrings = []struct {
	sub string
	id  string
}{
	{"deepseek-tui", "deepseek-tui"},
	{"cursor", "cursor"},
	{"opencode", "opencode"},
	{"openclaw", "openclaw"},
	{"kiro", "kiro"},
	{"antigravity", "antigravity"},
	{"hermes", "hermes"},
	{"droid", "droid"},
	{"factory", "droid"},
	{"kilocode", "kilo"},
	{"kilo-code", "kilo"},
	{"kilo", "kilo"},
	{"roo-code", "roo"},
	{"roo", "roo"},
	{"continue", "continue"},
	{"sourcegraph", "amp"},
	{"amp-cli", "amp"},
	{"amp/", "amp"},
	{" amp ", "amp"},
	{"qwen-code", "qwen"},
	{"qwen", "qwen"},
	{"grok", "grok"},
	{"xai", "grok"},
	{"jcode", "jcode"},
	{"cline", "cline"},
	{"cowork", "cowork"},
	{"codex", "codex"},
}

func matchAgentUserAgent(ua string) string {
	for _, item := range knownUASubstrings {
		if strings.Contains(ua, item.sub) {
			return item.id
		}
	}

	return ""
}

func isCopilotHeader(r *http.Request, ua string) bool {
	openaiIntent := strings.ToLower(r.Header.Get("openai-intent"))
	initiator := strings.ToLower(r.Header.Get("x-initiator"))

	if strings.Contains(ua, "githubcopilotchat") || openaiIntent == "conversation-panel" {
		return true
	}

	if initiator == "user" && (strings.Contains(ua, "copilot") || strings.Contains(ua, "vscode") || openaiIntent != "") {
		return true
	}

	return false
}

func matchSpecialHeaders(r *http.Request, ua string) string {
	if isCopilotHeader(r, ua) {
		return "github-copilot"
	}

	xApp := strings.ToLower(r.Header.Get("x-app"))
	if strings.Contains(ua, "claude-cli") || strings.Contains(ua, "claude-code") || xApp == "cli" {
		return "claude-code"
	}

	if strings.Contains(ua, "gemini-cli") {
		return "gemini-cli"
	}

	if strings.Contains(ua, "codex-cli") || strings.Contains(ua, "codex/") {
		return "codex"
	}

	return ""
}

// DetectClient identifies the client tool from request headers.
// Returns ids such as "claude-code", "cursor", "codex", "opencode", "kiro", etc.
func DetectClient(r *http.Request) string {
	if r == nil {
		return ""
	}

	ua := strings.ToLower(r.UserAgent())
	if id := matchSpecialHeaders(r, ua); id != "" {
		return id
	}

	if matched := matchAgentUserAgent(ua); matched != "" {
		return matched
	}

	if r.Header.Get("x-stainless-os") != "" || r.Header.Get("x-stainless-lang") != "" {
		return "codex"
	}

	if strings.EqualFold(r.Header.Get("x-client"), "cursor") {
		return "cursor"
	}

	return ""
}

func isClaudeClient(client, provider string) bool {
	return (client == "claude-code" || client == "claude") && (provider == "claude" || provider == "anthropic")
}

func isCopilotClient(client, provider string) bool {
	return (client == "github-copilot" || client == "copilot") && (provider == "github" || provider == "copilot" || provider == "github-copilot")
}

// ShouldPassthrough returns true if client format matches provider format
// (skip translation for native client→provider pairs).
func ShouldPassthrough(client, provider string) bool {
	if client == "" || provider == "" {
		return false
	}

	if strings.HasPrefix(provider, "anthropic-compatible") {
		provider = "anthropic"
	}

	if isClaudeClient(client, provider) || isCopilotClient(client, provider) {
		return true
	}

	return client == provider
}
