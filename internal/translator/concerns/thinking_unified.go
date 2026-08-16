package concerns

import "strings"

var formatToNative = map[string]string{
	"openai":           "openai",
	"openai-responses": "openai",
	"openai-response":  "openai",
	"codex":            "openai",
	"claude":           "claude-budget",
	"gemini":           "gemini-budget",
	"gemini-cli":       "gemini-budget",
	"vertex":           "gemini-budget",
	"antigravity":      "gemini-budget",
	"kiro":             "kiro",
}

var geminiLevelOutputFloor = map[string]int{
	"minimal": 4096,
	"low":     8192,
	"medium":  16384,
	"high":    65535,
}

func resolveThinkingFormat(targetFormat, model, provider string) string {
	if provider != "" {
		if fmt, ok := ProviderThinkingFormats[provider]; ok && fmt != "" {
			return fmt
		}

		if strings.HasPrefix(provider, "anthropic-compatible") {
			return "claude-budget"
		}
	}

	caps := GetCapabilitiesForModel(provider, model)
	if caps.ThinkingFormat != "" {
		return caps.ThinkingFormat
	}

	if fmt, ok := formatToNative[targetFormat]; ok {
		return fmt
	}

	return "openai"
}

func toBudget(cfg map[string]any, rng *ThinkingRange) (budget int, ok bool, auto bool) {
	mode, _ := cfg["mode"].(string)
	if mode == "auto" {
		return -1, true, true
	}

	if mode == "budget" {
		switch b := cfg["budget"].(type) {
		case int:
			budget = b
		case float64:
			budget = int(b)
		default:
			return 0, false, false
		}
	} else if mode == "level" {
		level, _ := cfg["level"].(string)

		b, found := EffortToBudget(level)
		if !found {
			return 0, false, false
		}

		budget = b
	} else {
		return 0, false, false
	}

	if rng != nil {
		if rng.Min > 0 && budget < rng.Min {
			budget = rng.Min
		}

		if rng.Max > 0 && budget > rng.Max {
			budget = rng.Max
		}
	}

	return budget, true, false
}

func toLevel(cfg map[string]any) string {
	mode, _ := cfg["mode"].(string)
	if mode == "level" {
		if l, ok := cfg["level"].(string); ok {
			return l
		}
	}

	if mode == "budget" {
		var budget int
		switch b := cfg["budget"].(type) {
		case int:
			budget = b
		case float64:
			budget = int(b)
		}

		if l := BudgetToLevel(budget); l != "" {
			return l
		}

		return "medium"
	}

	if mode == "auto" {
		return "auto"
	}

	return ""
}

func toGeminiThinkingLevel(cfg map[string]any) string {
	raw := "high"

	if mode, _ := cfg["mode"].(string); mode != "auto" {
		if l := toLevel(cfg); l != "" {
			raw = l
		}
	}

	return EffortToThinkingLevel(raw)
}

func toKimiReasoningEffort(cfg map[string]any) string {
	level := toLevel(cfg)
	switch level {
	case "auto":
		return "high"
	case "minimal":
		return "low"
	case "xhigh":
		return "max"
	case "low", "medium", "high", "max":
		return level
	}

	return ""
}

func geminiBudgetOutputFloor(budget int) int {
	if budget == -1 || budget == 0 {
		return 32768
	}

	if budget <= 1024 {
		return 8192
	}

	if budget <= 8192 {
		return 16384
	}

	if budget <= 24576 {
		return 32768
	}

	return 65535
}

func getGeminiGenerationConfig(body map[string]any) map[string]any {
	if req, ok := body["request"].(map[string]any); ok {
		gc, ok := req["generationConfig"].(map[string]any)
		if !ok {
			gc = map[string]any{}
			req["generationConfig"] = gc
		}

		return gc
	}

	gc, ok := body["generationConfig"].(map[string]any)
	if !ok {
		gc = map[string]any{}
		body["generationConfig"] = gc
	}

	return gc
}

func setGeminiThinking(body map[string]any, tc map[string]any) {
	gc := getGeminiGenerationConfig(body)
	gc["thinkingConfig"] = tc
}

func ensureGeminiOutputFloor(body map[string]any, floor int, caps *Capabilities) {
	target := floor
	if caps != nil && caps.MaxOutput > 0 && caps.MaxOutput < floor {
		target = caps.MaxOutput
	}

	gc := getGeminiGenerationConfig(body)
	current := 0

	switch v := gc["maxOutputTokens"].(type) {
	case float64:
		current = int(v)
	case int:
		current = v
	}

	if current == 0 || current < target {
		gc["maxOutputTokens"] = target
	}
}

func stripAllThinking(body map[string]any) {
	delete(body, "thinking")
	delete(body, "reasoning_effort")
	delete(body, "reasoning")
	delete(body, "thinkingConfig")
	delete(body, "enable_thinking")
	delete(body, "thinking_budget")
	delete(body, "output_config")

	if gc, ok := body["generationConfig"].(map[string]any); ok {
		delete(gc, "thinkingConfig")
	}

	if req, ok := body["request"].(map[string]any); ok {
		if gc, ok := req["generationConfig"].(map[string]any); ok {
			delete(gc, "thinkingConfig")
		}
	}
}

func applyThinkingFormat(fmt string, body map[string]any, cfg map[string]any, caps *Capabilities) {
	mode, _ := cfg["mode"].(string)
	none := mode == "none"
	canDisable := caps == nil || caps.ThinkingCanDisable
	eff := cfg

	if none && !canDisable {
		eff = map[string]any{"mode": "level", "level": "minimal"}
		none = false
	}

	switch fmt {
	case "openai":
		if none && canDisable {
			body["reasoning_effort"] = "none"
			return
		}

		level := toLevel(eff)
		if level == "max" {
			level = "xhigh"
		}

		if level != "" {
			body["reasoning_effort"] = level
		}
	case "claude-adaptive":
		if none && canDisable {
			body["thinking"] = map[string]any{"type": "disabled"}
			return
		}

		body["thinking"] = map[string]any{"type": "adaptive"}

		level := toLevel(eff)
		if level == "xhigh" {
			level = "high"
		}

		body["output_config"] = map[string]any{"effort": level}
	case "claude-budget":
		if none && canDisable {
			body["thinking"] = map[string]any{"type": "disabled"}
			return
		}

		budget, ok, auto := toBudget(eff, capsThinkingRange(caps))
		if auto || (ok && budget == -1) {
			body["thinking"] = map[string]any{"type": "enabled"}
		} else {
			if !ok || budget <= 0 {
				budget = 8192
			}

			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		}
	case "gemini-level":
		level := "minimal"
		if !none {
			level = toGeminiThinkingLevel(eff)
		}

		setGeminiThinking(body, map[string]any{
			"thinkingLevel":   level,
			"includeThoughts": level != "minimal",
		})

		floor := geminiLevelOutputFloor[level]
		if floor == 0 {
			floor = geminiLevelOutputFloor["high"]
		}

		ensureGeminiOutputFloor(body, floor, caps)
	case "gemini-budget":
		if none && canDisable {
			setGeminiThinking(body, map[string]any{"thinkingBudget": 0, "includeThoughts": false})
			return
		}

		budget, ok, auto := toBudget(eff, capsThinkingRange(caps))
		tb := -1

		if ok && !auto {
			tb = budget
		}

		setGeminiThinking(body, map[string]any{"thinkingBudget": tb, "includeThoughts": true})
		ensureGeminiOutputFloor(body, geminiBudgetOutputFloor(tb), caps)
	case "zai":
		if none && canDisable {
			body["enable_thinking"] = false
			delete(body, "thinking")

			return
		}

		body["thinking"] = map[string]any{"type": "enabled"}
	case "qwen":
		if none && canDisable {
			body["enable_thinking"] = false
			return
		}

		body["enable_thinking"] = true
		budget, ok, auto := toBudget(eff, capsThinkingRange(caps))

		if ok && !auto && budget > 0 {
			body["thinking_budget"] = budget
		}
	case "deepseek":
		if none && canDisable {
			body["thinking"] = map[string]any{"type": "disabled"}
			return
		}

		body["thinking"] = map[string]any{"type": "enabled"}
		level := toLevel(eff)

		if level == "xhigh" || level == "max" {
			body["reasoning_effort"] = "max"
		} else {
			body["reasoning_effort"] = "high"
		}
	case "kimi":
		if none && canDisable {
			body["thinking"] = map[string]any{"type": "disabled"}
			return
		}

		if effort := toKimiReasoningEffort(eff); effort != "" {
			body["reasoning_effort"] = effort
		}
	case "minimax":
		t := "adaptive"
		if none && canDisable {
			t = "disabled"
		}

		body["thinking"] = map[string]any{"type": t}
	case "hunyuan":
		if none && canDisable {
			body["thinking"] = map[string]any{"type": "disabled"}
			return
		}

		budget, ok, auto := toBudget(eff, capsThinkingRange(caps))
		if auto || (ok && budget == -1) {
			body["thinking"] = map[string]any{"type": "enabled"}
		} else {
			if !ok || budget <= 0 {
				budget = 8192
			}

			body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
		}
	case "step":
		if none && canDisable {
			return
		}

		level := toLevel(eff)
		if level != "" {
			if level == "xhigh" || level == "max" {
				level = "high"
			}

			body["reasoning_effort"] = level
		}
	case "kiro":
		// handled via system-tag injection in openai-to-kiro
	}
}

func capsThinkingRange(caps *Capabilities) *ThinkingRange {
	if caps == nil {
		return nil
	}

	return caps.ThinkingRange
}

// ApplyThinking normalizes thinking for resolved target format. Mutates body.
func ApplyThinking(targetFormat, model string, body map[string]any, provider string, intent map[string]any) map[string]any {
	if body == nil {
		return body
	}

	cleanModel, override := ParseSuffix(model)

	cfg := override
	if cfg == nil {
		cfg = intent
	}

	if cfg == nil {
		cfg = CaptureThinking(body)
	}

	caps := GetCapabilitiesForModel(provider, cleanModel)
	if !caps.Reasoning {
		stripAllThinking(body)
		return body
	}

	if cfg == nil {
		return body
	}

	fmt := resolveThinkingFormat(targetFormat, cleanModel, provider)

	stripAllThinking(body)
	applyThinkingFormat(fmt, body, cfg, caps)

	return body
}
