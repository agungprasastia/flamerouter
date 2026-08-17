// Package provider defines model provider configurations, registry listings,
// and model capabilities for all supported AI backends.
package provider

// DefaultCapabilities specifies baseline capabilities for models without specific overrides.
var DefaultCapabilities = Capabilities{
	ThinkingFormat: "",
	ContextWindow:  200000,
	MaxOutput:      64000,
	Vision:         false,
	PDF:            false,
	AudioInput:     false,
	VideoInput:     false,
	ImageOutput:    false,
	AudioOutput:    false,
	Search:         false,
	Tools:          true,
	Reasoning:      false,
}

func caps(vision, audioIn, audioOut, search, tools, reasoning bool, cw, maxOut int) Capabilities {
	return Capabilities{
		ThinkingFormat: "",
		ContextWindow:  cw,
		MaxOutput:      maxOut,
		Vision:         vision,
		PDF:            false,
		AudioInput:     audioIn,
		VideoInput:     false,
		ImageOutput:    false,
		AudioOutput:    audioOut,
		Search:         search,
		Tools:          tools,
		Reasoning:      reasoning,
	}
}

// ModelCapabilities maps known model IDs to explicit capabilities.
var ModelCapabilities = map[string]Capabilities{
	"claude-opus-4.6":   caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-opus-4.7":   caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-opus-4-7":   caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-opus-4.8":   caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-opus-4-6":   caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-opus-4-8":   caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-sonnet-4.6": caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-sonnet-4-6": caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-sonnet-5":   caps(true, false, false, true, false, true, 1000000, 128000),
	"claude-3-7-sonnet": caps(true, false, false, true, false, true, 200000, 128000),
	"claude-3-5-sonnet": caps(true, false, false, false, true, false, 200000, 8192),
	"gpt-5":             caps(true, false, false, true, false, true, 256000, 128000),
	"gpt-5-mini":        caps(true, false, false, true, false, true, 128000, 65536),
	"gpt-4o":            caps(true, false, false, false, true, false, 128000, 16384),
	"gpt-4o-mini":       caps(true, false, false, false, true, false, 128000, 16384),
	"gpt-4-turbo":       caps(true, false, false, false, true, false, 128000, 4096),
	"o3":                caps(true, false, false, false, false, true, 200000, 100000),
	"o3-mini":           caps(true, false, false, false, false, true, 200000, 100000),
	"o1":                caps(true, false, false, false, false, true, 200000, 100000),
	"gemini-2.5-pro":    caps(true, false, false, true, false, true, 1048576, 65536),
	"gemini-2.5-flash":  caps(true, false, false, true, false, true, 1048576, 65536),
	"gemini-2.0-flash":  caps(true, false, false, true, false, true, 1048576, 8192),
	"deepseek-r1":       caps(false, false, false, false, false, true, 128000, 8192),
	"deepseek-v3":       caps(false, false, false, false, true, false, 128000, 8192),
	"grok-3":            caps(true, false, false, true, false, true, 131072, 16384),
	"grok-3-mini":       caps(true, false, false, false, false, true, 131072, 16384),
	"llama-3.1-405b":    caps(false, false, false, false, true, false, 131072, 4096),
	"mistral-large":     caps(false, false, false, false, true, false, 128000, 4096),
	"qwen-max":          caps(false, false, false, false, true, false, 128000, 8192),
	"command-r-plus":    caps(false, false, false, false, true, false, 128000, 4096),
}

// PatternCapabilities defines wildcard patterns and their corresponding capabilities.
var PatternCapabilities = []struct {
	Pattern string
	Caps    Capabilities
}{
	{Pattern: "claude-*", Caps: caps(true, false, false, false, true, false, 200000, 8192)},
	{Pattern: "gpt-4*", Caps: caps(true, false, false, false, true, false, 128000, 4096)},
	{Pattern: "gpt-5*", Caps: caps(true, false, false, true, false, true, 256000, 128000)},
	{Pattern: "o*", Caps: caps(true, false, false, false, false, true, 200000, 100000)},
	{Pattern: "gemini-*", Caps: caps(true, false, false, false, true, false, 1048576, 65536)},
	{Pattern: "deepseek-*", Caps: caps(false, false, false, false, true, false, 128000, 8192)},
	{Pattern: "grok-*", Caps: caps(true, false, false, true, true, false, 131072, 16384)},
	{Pattern: "llama-*", Caps: caps(false, false, false, false, true, false, 131072, 4096)},
	{Pattern: "mistral-*", Caps: caps(false, false, false, false, true, false, 128000, 4096)},
	{Pattern: "qwen-*", Caps: caps(false, false, false, false, true, false, 128000, 8192)},
	{Pattern: "text-embedding-*", Caps: caps(false, false, false, false, false, false, 8191, 0)},
	{Pattern: "whisper-*", Caps: caps(false, true, false, false, false, false, 0, 0)},
	{Pattern: "tts-*", Caps: caps(false, false, true, false, false, false, 0, 0)},
}

// GetCapabilities resolves feature capabilities for a given model identifier.
func GetCapabilities(model string) Capabilities {
	if caps, ok := ModelCapabilities[model]; ok {
		return caps
	}

	for _, p := range PatternCapabilities {
		if matchGlob(model, p.Pattern) {
			return p.Caps
		}
	}

	return DefaultCapabilities
}

func matchGlob(name, pattern string) bool {
	if pattern == "*" {
		return true
	}

	if len(pattern) == 0 {
		return false
	}

	if pattern[len(pattern)-1] == '*' {
		return len(name) >= len(pattern)-1 && name[:len(pattern)-1] == pattern[:len(pattern)-1]
	}

	if pattern[0] == '*' {
		return len(name) >= len(pattern)-1 && name[len(name)-len(pattern)+1:] == pattern[1:]
	}

	return name == pattern
}
