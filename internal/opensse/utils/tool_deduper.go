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
		triggers: []string{"mcp__exa__web_search_exa", "mcp__exa__web_fetch_exa"},
		strip:    []string{"WebSearch", "WebFetch", "mcp__workspace__web_fetch"},
	},
	{
		triggers: []string{"mcp__tavily__tavily_search", "mcp__tavily__tavily_extract"},
		strip:    []string{"WebSearch", "WebFetch", "mcp__workspace__web_fetch"},
	},
	{
		triggerRes: []*regexp.Regexp{regexp.MustCompile(`^mcp__browsermcp__`)},
		stripRes:   []*regexp.Regexp{regexp.MustCompile(`^mcp__Claude_in_Chrome__`)},
	},
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

	// 1) Exact-name dedupe: keep later (MCP often appended after built-ins).
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

	// 2) Rule-based strip when MCP triggers present.
	names := make([]string, len(afterName))
	for i, t := range afterName {
		names[i] = toolName(t)
	}

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

	if len(toStrip) == 0 && len(afterName) == len(tools) {
		// No change: still re-marshal only if name-dedupe shrank the list.
		// Compare lengths — if same and no strip, return original body when no name dups.
		if !hadNameDup(tools) {
			return body
		}
	}

	result := make([]any, 0, len(afterName))

	for _, t := range afterName {
		n := toolName(t)
		if toStrip[n] {
			continue
		}

		result = append(result, t)
	}

	req["tools"] = result

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
	tm, _ := t.(map[string]any)
	if tm == nil {
		return ""
	}

	if name, _ := tm["name"].(string); name != "" {
		return name
	}

	fn, _ := tm["function"].(map[string]any)
	if fn == nil {
		return ""
	}

	name, _ := fn["name"].(string)

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
