package utils

import (
	"encoding/json"
	"regexp"
)

type dedupeRule struct {
	triggers   []string // exact names; empty means use triggerRes
	triggerRes []*regexp.Regexp
	strip      []string
	stripRes   []*regexp.Regexp
}

// Parity with 9router open-sse/utils/toolDeduper.js DEDUP_RULES.
var dedupeRules = []dedupeRule{
	{
		triggers:   []string{"mcp__exa__web_search_exa", "mcp__exa__web_fetch_exa"},
		triggerRes: nil,
		strip:      []string{"WebSearch", "WebFetch", "mcp__workspace__web_fetch"},
		stripRes:   nil,
	},
	{
		triggers:   []string{"mcp__tavily__tavily_search", "mcp__tavily__tavily_extract"},
		triggerRes: nil,
		strip:      []string{"WebSearch", "WebFetch", "mcp__workspace__web_fetch"},
		stripRes:   nil,
	},
	{
		triggers:   nil,
		triggerRes: []*regexp.Regexp{regexp.MustCompile(`^mcp__browsermcp__`)},
		strip:      nil,
		stripRes:   []*regexp.Regexp{regexp.MustCompile(`^mcp__Claude_in_Chrome__`)},
	},
}

func dedupeByName(tools []any) []any {
	seen := map[string]int{}
	deduped := make([]any, 0, len(tools))

	for _, t := range tools {
		name := toolName(t)
		if name != "" {
			if prev, exists := seen[name]; exists {
				deduped[prev] = nil
			}

			seen[name] = len(deduped)
		}

		deduped = append(deduped, t)
	}

	var afterName []any

	for _, t := range deduped {
		if t != nil {
			afterName = append(afterName, t)
		}
	}

	return afterName
}

func collectStripSet(names []string) map[string]bool {
	toStrip := map[string]bool{}

	for _, rule := range dedupeRules {
		if !ruleHasTrigger(names, rule) {
			continue
		}

		for _, n := range names {
			if ruleMatchesStrip(n, rule) {
				toStrip[n] = true
			}
		}
	}

	return toStrip
}

func filterStrippedTools(tools []any, toStrip map[string]bool) []any {
	result := make([]any, 0, len(tools))

	for _, t := range tools {
		n := toolName(t)
		if !toStrip[n] {
			result = append(result, t)
		}
	}

	return result
}

// DedupeTools removes built-in tools when equivalent MCP tools are present.
// Also drops exact name duplicates, keeping the later occurrence (MCP preferred).
func DedupeTools(body []byte) []byte {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return body
	}

	tools, ok := req["tools"].([]any)
	if !ok || len(tools) == 0 {
		return body
	}

	afterName := dedupeByName(tools)

	names := make([]string, len(afterName))
	for i, t := range afterName {
		names[i] = toolName(t)
	}

	toStrip := collectStripSet(names)

	if len(toStrip) == 0 && len(afterName) == len(tools) && !hadNameDup(tools) {
		return body
	}

	req["tools"] = filterStrippedTools(afterName, toStrip)

	out, err := json.Marshal(req)
	if err != nil {
		return body
	}

	return out
}

func hadNameDup(tools []any) bool {
	seen := map[string]bool{}

	for _, t := range tools {
		n := toolName(t)
		if n == "" {
			continue
		}

		if seen[n] {
			return true
		}

		seen[n] = true
	}

	return false
}

func toolName(t any) string {
	tm, ok1 := t.(map[string]any)
	if !ok1 || tm == nil {
		return ""
	}

	if name, ok2 := tm["name"].(string); ok2 && name != "" {
		return name
	}

	fn, ok3 := tm["function"].(map[string]any)
	if !ok3 || fn == nil {
		return ""
	}

	name, ok4 := fn["name"].(string)
	if !ok4 {
		return ""
	}

	return name
}

func ruleHasTrigger(names []string, rule dedupeRule) bool {
	for _, n := range names {
		for _, t := range rule.triggers {
			if n == t {
				return true
			}
		}

		for _, re := range rule.triggerRes {
			if re.MatchString(n) {
				return true
			}
		}
	}

	return false
}

func ruleMatchesStrip(name string, rule dedupeRule) bool {
	for _, s := range rule.strip {
		if name == s {
			return true
		}
	}

	for _, re := range rule.stripRes {
		if re.MatchString(name) {
			return true
		}
	}

	return false
}
