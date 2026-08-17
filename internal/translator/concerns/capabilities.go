package concerns

import "strings"

// ThinkingRange defines min and max values for thinking.
type ThinkingRange struct {
	Min int
	Max int
}

// Capabilities defines the features and limits supported by a model.
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
		ThinkingRange:      nil,
		ThinkingFormat:     "",
		MaxOutput:          64000,
		ContextWindow:      200000,
		PDF:                false,
		VideoInput:         false,
		Tools:              true,
		Reasoning:          false,
		Search:             false,
		ImageOutput:        false,
		ThinkingCanDisable: true,
		Vision:             false,
		AudioOutput:        false,
		AudioInput:         false,
	}
}

var modelCapabilities = map[string]*Capabilities{
	"claude-opus-4.6":   {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-opus-4.7":   {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-opus-4-7":   {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-opus-4.8":   {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-opus-4-6":   {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-opus-4-8":   {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-sonnet-4.6": {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-sonnet-4-6": {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-sonnet-5":   {ThinkingRange: nil, ThinkingFormat: "claude-adaptive", MaxOutput: 128000, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-3-7-sonnet": {ThinkingRange: nil, ThinkingFormat: "claude-budget", MaxOutput: 128000, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"claude-3-5-sonnet": {ThinkingRange: nil, ThinkingFormat: "", MaxOutput: 8192, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: false, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"gpt-5":             {ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 128000, ContextWindow: 256000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"gpt-5-mini":        {ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 65536, ContextWindow: 128000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"gpt-4o":            {ThinkingRange: nil, ThinkingFormat: "", MaxOutput: 16384, ContextWindow: 128000, PDF: false, VideoInput: false, Tools: true, Reasoning: false, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"o3":                {ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 100000, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"o3-mini":           {ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 100000, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"o1":                {ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 100000, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"gemini-2.5-pro":    {ThinkingRange: nil, ThinkingFormat: "gemini-budget", MaxOutput: 65536, ContextWindow: 1048576, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"gemini-2.5-flash":  {ThinkingRange: nil, ThinkingFormat: "gemini-budget", MaxOutput: 65536, ContextWindow: 1048576, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"gemini-2.0-flash":  {ThinkingRange: nil, ThinkingFormat: "gemini-budget", MaxOutput: 8192, ContextWindow: 1048576, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"deepseek-r1":       {ThinkingRange: nil, ThinkingFormat: "deepseek", MaxOutput: 8192, ContextWindow: 128000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: false, AudioOutput: false, AudioInput: false},
	"deepseek-v3":       {ThinkingRange: nil, ThinkingFormat: "", MaxOutput: 8192, ContextWindow: 128000, PDF: false, VideoInput: false, Tools: true, Reasoning: false, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: false, AudioOutput: false, AudioInput: false},
	"kimi-k3":           {ThinkingRange: nil, ThinkingFormat: "kimi", MaxOutput: 131072, ContextWindow: 1048576, PDF: false, VideoInput: true, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: false, Vision: true, AudioOutput: false, AudioInput: false},
	"k3":                {ThinkingRange: nil, ThinkingFormat: "kimi", MaxOutput: 131072, ContextWindow: 1048576, PDF: false, VideoInput: true, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: false, Vision: true, AudioOutput: false, AudioInput: false},
	"kimi-for-coding":   {ThinkingRange: nil, ThinkingFormat: "kimi", MaxOutput: 65536, ContextWindow: 262144, PDF: false, VideoInput: true, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: false, Vision: true, AudioOutput: false, AudioInput: false},
	"glm-4.6v":          {ThinkingRange: nil, ThinkingFormat: "zai", MaxOutput: 0, ContextWindow: 128000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"vision-model":      {ThinkingRange: nil, ThinkingFormat: "qwen", MaxOutput: 0, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"coder-model":       {ThinkingRange: nil, ThinkingFormat: "qwen", MaxOutput: 0, ContextWindow: 1000000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: false, AudioOutput: false, AudioInput: false},
	"grok-3":            {ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 16384, ContextWindow: 131072, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
	"grok-3-mini":       {ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 16384, ContextWindow: 131072, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false},
}

var patternCapabilities = []struct {
	caps   *Capabilities
	prefix string
}{
	{prefix: "claude-", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "claude-budget", MaxOutput: 8192, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: false, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false}},
	{prefix: "gpt-5", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 128000, ContextWindow: 256000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false}},
	{prefix: "gpt-4", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "", MaxOutput: 4096, ContextWindow: 128000, PDF: false, VideoInput: false, Tools: true, Reasoning: false, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false}},
	{prefix: "o1", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 100000, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false}},
	{prefix: "o3", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 100000, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false}},
	{prefix: "gemini-", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "gemini-budget", MaxOutput: 65536, ContextWindow: 1048576, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false}},
	{prefix: "deepseek-", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "deepseek", MaxOutput: 8192, ContextWindow: 128000, PDF: false, VideoInput: false, Tools: true, Reasoning: false, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: false, AudioOutput: false, AudioInput: false}},
	{prefix: "kimi-", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "kimi", MaxOutput: 65536, ContextWindow: 262144, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: false, Vision: true, AudioOutput: false, AudioInput: false}},
	{prefix: "glm-", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "zai", MaxOutput: 48000, ContextWindow: 200000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: false, AudioOutput: false, AudioInput: false}},
	{prefix: "qwen-", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "qwen", MaxOutput: 8192, ContextWindow: 128000, PDF: false, VideoInput: false, Tools: true, Reasoning: false, Search: false, ImageOutput: false, ThinkingCanDisable: true, Vision: false, AudioOutput: false, AudioInput: false}},
	{prefix: "minimax-", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "minimax", MaxOutput: 48000, ContextWindow: 512000, PDF: false, VideoInput: false, Tools: true, Reasoning: true, Search: false, ImageOutput: false, ThinkingCanDisable: false, Vision: true, AudioOutput: false, AudioInput: false}},
	{prefix: "grok-", caps: &Capabilities{ThinkingRange: nil, ThinkingFormat: "openai", MaxOutput: 16384, ContextWindow: 131072, PDF: false, VideoInput: false, Tools: true, Reasoning: false, Search: true, ImageOutput: false, ThinkingCanDisable: true, Vision: true, AudioOutput: false, AudioInput: false}},
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

// GetCapabilitiesForModel resolves capabilities for a given model.
func GetCapabilitiesForModel(_ /* provider */, model string) *Capabilities {
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
