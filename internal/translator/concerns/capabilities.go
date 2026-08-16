package concerns

import "strings"

type ThinkingRange struct {
	Min int
	Max int
}

type Capabilities struct {
	ThinkingRange      *ThinkingRange
	ThinkingFormat     string
	MaxOutput          int
	ContextWindow      int
	PDF                bool
	VideoInput         bool
	Tools              bool
	Reasoning          bool
	Search             bool
	ImageOutput        bool
	ThinkingCanDisable bool
	Vision             bool
	AudioOutput        bool
	AudioInput         bool
}

func defaultCaps() *Capabilities {
	return &Capabilities{
		Tools:              true,
		ThinkingCanDisable: true,
		ContextWindow:      200000,
		MaxOutput:          64000,
	}
}

var modelCapabilities = map[string]*Capabilities{
	"claude-opus-4.6":   {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-opus-4.7":   {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-opus-4-7":   {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-opus-4.8":   {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-opus-4-6":   {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-opus-4-8":   {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-sonnet-4.6": {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-sonnet-4-6": {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-sonnet-5":   {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-adaptive", ContextWindow: 1000000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-3-7-sonnet": {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "claude-budget", ContextWindow: 200000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"claude-3-5-sonnet": {Vision: true, Tools: true, ContextWindow: 200000, MaxOutput: 8192, ThinkingCanDisable: true},
	"gpt-5":             {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "openai", ContextWindow: 256000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true},
	"gpt-5-mini":        {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "openai", ContextWindow: 128000, MaxOutput: 65536, ThinkingCanDisable: true, Tools: true},
	"gpt-4o":            {Vision: true, Tools: true, ContextWindow: 128000, MaxOutput: 16384, ThinkingCanDisable: true},
	"o3":                {Vision: true, Reasoning: true, ThinkingFormat: "openai", ContextWindow: 200000, MaxOutput: 100000, ThinkingCanDisable: true, Tools: true},
	"o3-mini":           {Vision: true, Reasoning: true, ThinkingFormat: "openai", ContextWindow: 200000, MaxOutput: 100000, ThinkingCanDisable: true, Tools: true},
	"o1":                {Vision: true, Reasoning: true, ThinkingFormat: "openai", ContextWindow: 200000, MaxOutput: 100000, ThinkingCanDisable: true, Tools: true},
	"gemini-2.5-pro":    {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "gemini-budget", ContextWindow: 1048576, MaxOutput: 65536, ThinkingCanDisable: true, Tools: true},
	"gemini-2.5-flash":  {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "gemini-budget", ContextWindow: 1048576, MaxOutput: 65536, ThinkingCanDisable: true, Tools: true},
	"gemini-2.0-flash":  {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "gemini-budget", ContextWindow: 1048576, MaxOutput: 8192, ThinkingCanDisable: true, Tools: true},
	"deepseek-r1":       {Reasoning: true, ThinkingFormat: "deepseek", ContextWindow: 128000, MaxOutput: 8192, ThinkingCanDisable: true, Tools: true},
	"deepseek-v3":       {Tools: true, ContextWindow: 128000, MaxOutput: 8192, ThinkingCanDisable: true},
	"kimi-k3":           {Vision: true, VideoInput: true, Reasoning: true, ThinkingFormat: "kimi", ThinkingCanDisable: false, ContextWindow: 1048576, MaxOutput: 131072, Tools: true},
	"k3":                {Vision: true, VideoInput: true, Reasoning: true, ThinkingFormat: "kimi", ThinkingCanDisable: false, ContextWindow: 1048576, MaxOutput: 131072, Tools: true},
	"kimi-for-coding":   {Vision: true, VideoInput: true, Reasoning: true, ThinkingFormat: "kimi", ThinkingCanDisable: false, ContextWindow: 262144, MaxOutput: 65536, Tools: true},
	"glm-4.6v":          {Vision: true, Reasoning: true, ThinkingFormat: "zai", ContextWindow: 128000, ThinkingCanDisable: true, Tools: true},
	"vision-model":      {Vision: true, Reasoning: true, ThinkingFormat: "qwen", ContextWindow: 1000000, ThinkingCanDisable: true, Tools: true},
	"coder-model":       {Reasoning: true, ThinkingFormat: "qwen", ContextWindow: 1000000, ThinkingCanDisable: true, Tools: true},
	"grok-3":            {Vision: true, Reasoning: true, Search: true, ThinkingFormat: "openai", ContextWindow: 131072, MaxOutput: 16384, ThinkingCanDisable: true, Tools: true},
	"grok-3-mini":       {Vision: true, Reasoning: true, ThinkingFormat: "openai", ContextWindow: 131072, MaxOutput: 16384, ThinkingCanDisable: true, Tools: true},
}

var patternCapabilities = []struct {
	prefix string
	caps   *Capabilities
}{
	{"claude-", &Capabilities{Vision: true, Tools: true, ThinkingFormat: "claude-budget", ContextWindow: 200000, MaxOutput: 8192, ThinkingCanDisable: true}},
	{"gpt-5", &Capabilities{Vision: true, Reasoning: true, Search: true, ThinkingFormat: "openai", ContextWindow: 256000, MaxOutput: 128000, ThinkingCanDisable: true, Tools: true}},
	{"gpt-4", &Capabilities{Vision: true, Tools: true, ContextWindow: 128000, MaxOutput: 4096, ThinkingCanDisable: true}},
	{"o1", &Capabilities{Vision: true, Reasoning: true, ThinkingFormat: "openai", ContextWindow: 200000, MaxOutput: 100000, ThinkingCanDisable: true, Tools: true}},
	{"o3", &Capabilities{Vision: true, Reasoning: true, ThinkingFormat: "openai", ContextWindow: 200000, MaxOutput: 100000, ThinkingCanDisable: true, Tools: true}},
	{"gemini-", &Capabilities{Vision: true, Tools: true, Reasoning: true, ThinkingFormat: "gemini-budget", ContextWindow: 1048576, MaxOutput: 65536, ThinkingCanDisable: true}},
	{"deepseek-", &Capabilities{Tools: true, ThinkingFormat: "deepseek", ContextWindow: 128000, MaxOutput: 8192, ThinkingCanDisable: true}},
	{"kimi-", &Capabilities{Vision: true, Reasoning: true, ThinkingFormat: "kimi", ThinkingCanDisable: false, ContextWindow: 262144, MaxOutput: 65536, Tools: true}},
	{"glm-", &Capabilities{Reasoning: true, ThinkingFormat: "zai", ContextWindow: 200000, MaxOutput: 48000, ThinkingCanDisable: true, Tools: true}},
	{"qwen-", &Capabilities{Tools: true, ThinkingFormat: "qwen", ContextWindow: 128000, MaxOutput: 8192, ThinkingCanDisable: true}},
	{"minimax-", &Capabilities{Vision: true, Reasoning: true, ThinkingFormat: "minimax", ThinkingCanDisable: false, ContextWindow: 512000, MaxOutput: 48000, Tools: true}},
	{"grok-", &Capabilities{Vision: true, Tools: true, Search: true, ThinkingFormat: "openai", ContextWindow: 131072, MaxOutput: 16384, ThinkingCanDisable: true}},
}

// ProviderThinkingFormats maps provider id → default thinking wire format.
var ProviderThinkingFormats = map[string]string{
	"claude":               "claude-budget",
	"anthropic-compatible": "claude-budget",
	"deepseek":             "deepseek",
	"gemini":               "gemini-budget",
	"gemini-cli":           "gemini-budget",
	"vertex":               "gemini-budget",
	"antigravity":          "gemini-budget",
	"kiro":                 "kiro",
	"zhipu":                "zai",
	"zai":                  "zai",
	"qwen":                 "qwen",
	"dashscope":            "qwen",
	"moonshot":             "kimi",
	"kimi":                 "kimi",
	"minimax":              "minimax",
	"hunyuan":              "hunyuan",
	"step":                 "step",
	"stepfun":              "step",
}

func GetCapabilitiesForModel(provider, model string) *Capabilities {
	clean, _ := ParseSuffix(model)
	if clean == "" {
		clean = model
	}

	if caps, ok := modelCapabilities[clean]; ok {
		c := *caps
		if !c.ThinkingCanDisable && !caps.ThinkingCanDisable {
			// keep false
		} else if !caps.Reasoning {
			c.ThinkingCanDisable = true
		}

		return &c
	}

	lower := strings.ToLower(clean)
	for _, p := range patternCapabilities {
		if strings.HasPrefix(lower, p.prefix) || strings.HasPrefix(clean, p.prefix) {
			c := *p.caps
			return &c
		}
	}

	return defaultCaps()
}
