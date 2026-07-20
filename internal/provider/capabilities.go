package provider

var DefaultCapabilities = Capabilities{
	Vision:        false,
	PDF:           false,
	AudioInput:    false,
	VideoInput:    false,
	ImageOutput:   false,
	AudioOutput:   false,
	Search:        false,
	Tools:         true,
	Reasoning:     false,
	ContextWindow: 200000,
	MaxOutput:     64000,
}

var ModelCapabilities = map[string]Capabilities{
	"claude-opus-4.6":   {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-opus-4.7":   {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-opus-4-7":   {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-opus-4.8":   {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-opus-4-6":   {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-opus-4-8":   {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-sonnet-4.6": {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-sonnet-4-6": {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-sonnet-5":   {Vision: true, Reasoning: true, Search: true, ContextWindow: 1000000, MaxOutput: 128000},
	"claude-3-7-sonnet": {Vision: true, Reasoning: true, Search: true, ContextWindow: 200000, MaxOutput: 128000},
	"claude-3-5-sonnet": {Vision: true, Tools: true, ContextWindow: 200000, MaxOutput: 8192},
	"gpt-5":             {Vision: true, Reasoning: true, Search: true, ContextWindow: 256000, MaxOutput: 128000},
	"gpt-5-mini":        {Vision: true, Reasoning: true, Search: true, ContextWindow: 128000, MaxOutput: 65536},
	"gpt-4o":            {Vision: true, Tools: true, ContextWindow: 128000, MaxOutput: 16384},
	"gpt-4o-mini":       {Vision: true, Tools: true, ContextWindow: 128000, MaxOutput: 16384},
	"gpt-4-turbo":       {Vision: true, Tools: true, ContextWindow: 128000, MaxOutput: 4096},
	"o3":                {Vision: true, Reasoning: true, ContextWindow: 200000, MaxOutput: 100000},
	"o3-mini":           {Vision: true, Reasoning: true, ContextWindow: 200000, MaxOutput: 100000},
	"o1":                {Vision: true, Reasoning: true, ContextWindow: 200000, MaxOutput: 100000},
	"gemini-2.5-pro":    {Vision: true, Reasoning: true, Search: true, ContextWindow: 1048576, MaxOutput: 65536},
	"gemini-2.5-flash":  {Vision: true, Reasoning: true, Search: true, ContextWindow: 1048576, MaxOutput: 65536},
	"gemini-2.0-flash":  {Vision: true, Reasoning: true, Search: true, ContextWindow: 1048576, MaxOutput: 8192},
	"deepseek-r1":       {Vision: false, Reasoning: true, ContextWindow: 128000, MaxOutput: 8192},
	"deepseek-v3":       {Vision: false, Tools: true, ContextWindow: 128000, MaxOutput: 8192},
	"grok-3":            {Vision: true, Reasoning: true, Search: true, ContextWindow: 131072, MaxOutput: 16384},
	"grok-3-mini":       {Vision: true, Reasoning: true, ContextWindow: 131072, MaxOutput: 16384},
	"llama-3.1-405b":    {Vision: false, Tools: true, ContextWindow: 131072, MaxOutput: 4096},
	"mistral-large":     {Vision: false, Tools: true, ContextWindow: 128000, MaxOutput: 4096},
	"qwen-max":          {Vision: false, Tools: true, ContextWindow: 128000, MaxOutput: 8192},
	"command-r-plus":    {Vision: false, Tools: true, ContextWindow: 128000, MaxOutput: 4096},
}

var PatternCapabilities = []struct {
	Pattern string
	Caps    Capabilities
}{
	{"claude-*", Capabilities{Vision: true, Tools: true, ContextWindow: 200000, MaxOutput: 8192}},
	{"gpt-4*", Capabilities{Vision: true, Tools: true, ContextWindow: 128000, MaxOutput: 4096}},
	{"gpt-5*", Capabilities{Vision: true, Reasoning: true, Search: true, ContextWindow: 256000, MaxOutput: 128000}},
	{"o*", Capabilities{Vision: true, Reasoning: true, ContextWindow: 200000, MaxOutput: 100000}},
	{"gemini-*", Capabilities{Vision: true, Tools: true, ContextWindow: 1048576, MaxOutput: 65536}},
	{"deepseek-*", Capabilities{Tools: true, ContextWindow: 128000, MaxOutput: 8192}},
	{"grok-*", Capabilities{Vision: true, Tools: true, Search: true, ContextWindow: 131072, MaxOutput: 16384}},
	{"llama-*", Capabilities{Tools: true, ContextWindow: 131072, MaxOutput: 4096}},
	{"mistral-*", Capabilities{Tools: true, ContextWindow: 128000, MaxOutput: 4096}},
	{"qwen-*", Capabilities{Tools: true, ContextWindow: 128000, MaxOutput: 8192}},
	{"text-embedding-*", Capabilities{Tools: false, ContextWindow: 8191, MaxOutput: 0}},
	{"whisper-*", Capabilities{AudioInput: true, Tools: false}},
	{"tts-*", Capabilities{AudioOutput: true, Tools: false}},
}

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
