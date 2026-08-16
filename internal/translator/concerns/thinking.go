package concerns

import "strings"

var EffortLevels = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

var LevelToBudget = map[string]int{
	"none":    0,
	"minimal": 512,
	"low":     1024,
	"medium":  8192,
	"high":    24576,
	"xhigh":   32768,
	"max":     128000,
}

func EffortToBudget(effort string) (int, bool) {
	if effort == "" {
		return 0, false
	}

	budget, ok := LevelToBudget[effort]

	return budget, ok
}

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
			if raw == "none" || raw == "off" {
				return cleanModel, map[string]any{"mode": "none"}
			}

			if raw == "auto" {
				return cleanModel, map[string]any{"mode": "auto"}
			}

			budget := 0

			for _, c := range raw {
				if c >= '0' && c <= '9' {
					budget = budget*10 + int(c-'0')
				} else {
					budget = 0
					break
				}
			}

			if budget > 0 {
				return cleanModel, map[string]any{"mode": "budget", "budget": budget}
			}

			if _, ok := LevelToBudget[raw]; ok {
				return cleanModel, map[string]any{"mode": "level", "level": raw}
			}

			return cleanModel, nil
		}
	}

	return model, nil
}
