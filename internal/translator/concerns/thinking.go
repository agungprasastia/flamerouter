package concerns

import "strings"

// EffortLevels lists valid reasoning effort levels.
var EffortLevels = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// LevelToBudget maps effort levels to token budgets.
var LevelToBudget = map[string]int{
	"none":    0,
	"minimal": 512,
	"low":     1024,
	"medium":  8192,
	"high":    24576,
	"xhigh":   32768,
	"max":     128000,
}

// EffortToBudget converts an effort level to a numeric token budget.
func EffortToBudget(effort string) (int, bool) {
	if effort == "" {
		return 0, false
	}

	budget, ok := LevelToBudget[effort]

	return budget, ok
}

// EffortToThinkingLevel maps effort to Gemini thinking level.
func EffortToThinkingLevel(effort string) string {
	e := effort
	if e == "none" || e == "off" {
		return "minimal"
	}

	if e == "xhigh" || e == "max" {
		return "high"
	}

	return e
}

// BudgetToLevel converts a token budget to an effort level.
func BudgetToLevel(budget int) string {
	if budget <= 0 {
		return ""
	}

	if budget <= 768 {
		return "minimal"
	}

	if budget <= 4096 {
		return "low"
	}

	if budget <= 16384 {
		return "medium"
	}

	if budget <= 28672 {
		return "high"
	}

	return "xhigh"
}

// BudgetToEffort converts a token budget to an OpenAI reasoning effort.
func BudgetToEffort(budget int) string {
	if budget <= 0 {
		return ""
	}

	if budget <= 2048 {
		return "low"
	}

	if budget <= 16384 {
		return "medium"
	}

	return "high"
}

// StripThinkingSuffix removes suffix like (thinking) or (8192) from model name.
func StripThinkingSuffix(model string) string {
	if model == "" {
		return model
	}

	for i := len(model) - 1; i >= 0; i-- {
		if model[i] == '(' {
			return model[:i]
		}

		if model[i] == ')' {
			return model
		}
	}

	return model
}

func parseNumericBudget(raw string) int {
	budget := 0

	for _, c := range raw {
		if c >= '0' && c <= '9' {
			budget = budget*10 + int(c-'0')
		} else {
			return 0
		}
	}

	return budget
}

func parseRawSuffixConfig(raw string) map[string]any {
	if raw == "none" || raw == "off" {
		return map[string]any{"mode": "none"}
	}

	if raw == "auto" {
		return map[string]any{"mode": "auto"}
	}

	if budget := parseNumericBudget(raw); budget > 0 {
		return map[string]any{"mode": "budget", "budget": budget}
	}

	if _, ok := LevelToBudget[raw]; ok {
		return map[string]any{"mode": "level", "level": raw}
	}

	return nil
}

// ParseSuffix extracts thinking mode/budget specified in model suffix.
func ParseSuffix(model string) (string, map[string]any) {
	if model == "" {
		return model, nil
	}

	for i := len(model) - 1; i >= 0; i-- {
		if model[i] == '(' {
			cleanModel := strings.TrimSpace(model[:i])
			rest := model[i+1:]

			if len(rest) > 0 && rest[len(rest)-1] == ')' {
				rest = rest[:len(rest)-1]
			}

			raw := strings.ToLower(strings.TrimSpace(rest))

			return cleanModel, parseRawSuffixConfig(raw)
		}
	}

	return model, nil
}
