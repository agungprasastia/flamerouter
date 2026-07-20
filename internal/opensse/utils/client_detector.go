package utils

import (
	"net/http"
	"strings"
)

// DetectClient identifies the client tool from request headers.
// Returns ids such as "claude-code", "cursor", "codex", "opencode", "kiro", etc.
func DetectClient(r *http.Request) string {
	if r == nil {
		return ""
	}
	ua := strings.ToLower(r.UserAgent())
	xApp := strings.ToLower(r.Header.Get("x-app"))
	openaiIntent := strings.ToLower(r.Header.Get("openai-intent"))
	initiator := strings.ToLower(r.Header.Get("x-initiator"))

	// Order matters: more specific UA checks first.
	switch {
	// Copilot: require UA or conversation-panel; x-initiator alone too broad.
	case strings.Contains(ua, "githubcopilotchat") || openaiIntent == "conversation-panel" ||
		(initiator == "user" && (strings.Contains(ua, "copilot") || strings.Contains(ua, "vscode") || openaiIntent != "")):
		return "github-copilot"
	case strings.Contains(ua, "claude-cli") || strings.Contains(ua, "claude-code") || xApp == "cli":
		return "claude-code"
	case strings.Contains(ua, "gemini-cli"):
		return "gemini-cli"
	// Clear codex markers before generic stainless fallback.
	case strings.Contains(ua, "codex-cli") || strings.Contains(ua, "codex/"):
		return "codex"
	case strings.Contains(ua, "deepseek-tui"):
		return "deepseek-tui"
	case strings.Contains(ua, "cursor"):
		return "cursor"
	case strings.Contains(ua, "opencode"):
		return "opencode"
	case strings.Contains(ua, "openclaw"):
		return "openclaw"
	case strings.Contains(ua, "kiro"):
		return "kiro"
	case strings.Contains(ua, "antigravity"):
		return "antigravity"
	case strings.Contains(ua, "hermes"):
		return "hermes"
	case strings.Contains(ua, "droid") || strings.Contains(ua, "factory"):
		return "droid"
	case strings.Contains(ua, "kilocode") || strings.Contains(ua, "kilo-code") || strings.Contains(ua, "kilo"):
		return "kilo"
	case strings.Contains(ua, "roo-code") || strings.Contains(ua, "roo"):
		return "roo"
	case strings.Contains(ua, "continue"):
		return "continue"
	case strings.Contains(ua, "sourcegraph") || strings.Contains(ua, "amp-cli") || strings.HasPrefix(ua, "amp/") || strings.Contains(ua, " amp "):
		return "amp"
	case strings.Contains(ua, "qwen-code") || strings.Contains(ua, "qwen"):
		return "qwen"
	case strings.Contains(ua, "grok") || strings.Contains(ua, "xai"):
		return "grok"
	case strings.Contains(ua, "jcode"):
		return "jcode"
	case strings.Contains(ua, "cline"):
		return "cline"
	case strings.Contains(ua, "cowork"):
		return "cowork"
	case strings.Contains(ua, "codex"):
		return "codex"
	}

	// Codex SDK often sets stainless headers without a distinctive UA (last resort).
	if r.Header.Get("x-stainless-os") != "" || r.Header.Get("x-stainless-lang") != "" {
		return "codex"
	}
	if strings.EqualFold(r.Header.Get("x-client"), "cursor") {
		return "cursor"
	}
	return ""
}

// ShouldPassthrough returns true if client format matches provider format
// (skip translation for native client→provider pairs).
func ShouldPassthrough(client, provider string) bool {
	if client == "" || provider == "" {
		return false
	}
	// anthropic-compatible-* → anthropic (parity with 9router isNativePassthrough)
	if strings.HasPrefix(provider, "anthropic-compatible") {
		provider = "anthropic"
	}
	switch client {
	case "claude-code", "claude":
		return provider == "claude" || provider == "anthropic"
	case "cursor":
		return provider == "cursor"
	case "codex":
		return provider == "codex"
	case "gemini-cli":
		return provider == "gemini-cli"
	case "antigravity":
		return provider == "antigravity"
	case "kiro":
		return provider == "kiro"
	case "opencode":
		return provider == "opencode"
	case "github-copilot", "copilot":
		return provider == "github" || provider == "copilot" || provider == "github-copilot"
	}
	return false
}
