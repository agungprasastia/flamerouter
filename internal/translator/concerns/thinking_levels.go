package concerns

import (
	"encoding/json"
	"strings"
)

const defaultThinkingBudget = 10000

// ApplyThinkingLevel handles `:thinking` model suffix and per-provider thinking config.
// For Claude: adds/updates thinking.budget_tokens
// For OpenAI: sets reasoning_effort
// For Gemini: sets thinkingConfig
// Returns modified body and cleaned model name.
func ApplyThinkingLevel(body []byte, model, provider, targetFormat string, providerThinking map[string]any) ([]byte, string) {
	cleanModel, fromSuffix := stripColonThinking(model)

	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil || m == nil {
		return body, cleanModel
	}

	if fromSuffix {
		if s, ok := m["model"].(string); ok && s != "" {
			cleaned, _ := stripColonThinking(s)
			m["model"] = cleaned
		}
	}

	cfg := resolveThinkingCfg(fromSuffix, m, providerThinking)
	if cfg == nil {
		out, err := json.Marshal(m)
		if err != nil {
			return body, cleanModel
		}

		return out, cleanModel
	}

	// Soft inject when client has no thinking (parity with chatCore providerThinking).
	// When :thinking suffix, apply format-native params (explicit request — skip Reasoning gate).
	if fromSuffix {
		caps := GetCapabilitiesForModel(provider, cleanModel)
		fmt := resolveThinkingFormat(targetFormat, cleanModel, provider)

		stripAllThinking(m)
		applyThinkingFormat(fmt, m, cfg, caps)
	} else {
		injectProviderThinking(m, cfg)
	}

	out, err := json.Marshal(m)
	if err != nil {
		return body, cleanModel
	}

	return out, cleanModel
}

func stripColonThinking(model string) (string, bool) {
	if model == "" {
		return model, false
	}
	// Also honor parenthetical suffix strip for model clean (ParseSuffix path).
	base, _ := ParseSuffix(model)
	if base == "" {
		base = model
	}

	if strings.HasSuffix(strings.ToLower(base), ":thinking") {
		return base[:len(base)-len(":thinking")], true
	}
	// Case: model already had (level) stripped but still ends with :thinking on original
	if strings.HasSuffix(strings.ToLower(model), ":thinking") {
		return model[:len(model)-len(":thinking")], true
	}

	return base, false
}

func resolveThinkingCfg(fromSuffix bool, body map[string]any, providerThinking map[string]any) map[string]any {
	if fromSuffix {
		return map[string]any{"mode": "budget", "budget": defaultThinkingBudget}
	}

	if providerThinking == nil {
		return nil
	}

	mode, _ := providerThinking["mode"].(string)
	if mode == "" || mode == "auto" {
		return nil
	}

	switch mode {
	case "on":
		return map[string]any{"mode": "budget", "budget": defaultThinkingBudget}
	case "off":
		// only off → thinking disabled (9router chatCore)
		return map[string]any{"mode": "off"}
	default:
		// none/low/medium/high/minimal/xhigh/max/thinking → effort path
		if mode == "thinking" {
			return map[string]any{"mode": "budget", "budget": defaultThinkingBudget}
		}

		return map[string]any{"mode": "level", "level": mode}
	}
}

// injectProviderThinking matches 9router chatCore soft inject:
// on → body.thinking enabled; off → body.thinking disabled; none/effort → body.reasoning_effort when unset.
// Per-field guards: on/off only if !body.thinking; effort only if !body.reasoning_effort.
func injectProviderThinking(body map[string]any, cfg map[string]any) {
	mode, _ := cfg["mode"].(string)
	switch mode {
	case "budget":
		if body["thinking"] != nil {
			return
		}

		budget := defaultThinkingBudget

		switch b := cfg["budget"].(type) {
		case int:
			if b > 0 {
				budget = b
			}
		case float64:
			if b > 0 {
				budget = int(b)
			}
		}

		body["thinking"] = map[string]any{"type": "enabled", "budget_tokens": budget}
	case "off":
		if body["thinking"] != nil {
			return
		}

		body["thinking"] = map[string]any{"type": "disabled"}
	case "level":
		if body["reasoning_effort"] != nil {
			return
		}

		level, _ := cfg["level"].(string)
		if level != "" {
			body["reasoning_effort"] = level
		}
	}
}
