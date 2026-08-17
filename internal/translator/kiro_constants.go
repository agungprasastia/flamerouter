// Package translator provides core translator registries, types, and format detection logic.
package translator

import (
	"strings"
)

// Kiro suffix and profile ARN constants.
const (
	KiroAgenticSuffix  = "-agentic"
	KiroThinkingSuffix = "-thinking"

	KiroThinkingBudgetDefault = 16000

	KiroDefaultProfileArnBuilderID = "arn:aws:codewhisperer:us-east-1:638616132270:profile/AAAACCCCXXXX"
	KiroDefaultProfileArnSocial    = "arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK"
)

// KiroAgenticSystemPrompt contains chunked write instructions for Kiro agentic models.
var KiroAgenticSystemPrompt = `
# CRITICAL: CHUNKED WRITE PROTOCOL (MANDATORY)

You MUST follow these rules for ALL file operations. Violation causes server timeouts and task failure.

## ABSOLUTE LIMITS
- **MAXIMUM 350 LINES** per single write/edit operation - NO EXCEPTIONS
- **RECOMMENDED 300 LINES** or less for optimal performance
- **NEVER** write entire files in one operation if >300 lines

## MANDATORY CHUNKED WRITE STRATEGY

### For NEW FILES (>300 lines total):
1. FIRST: Write initial chunk (first 250-300 lines) using write_to_file/fsWrite
2. THEN: Append remaining content in 250-300 line chunks using file append operations
3. REPEAT: Continue appending until complete

### For EDITING EXISTING FILES:
1. Use surgical edits (apply_diff/targeted edits) - change ONLY what's needed
2. NEVER rewrite entire files - use incremental modifications
3. Split large refactors into multiple small, focused edits

### For LARGE CODE GENERATION:
1. Generate in logical sections (imports, types, functions separately)
2. Write each section as a separate operation
3. Use append operations for subsequent sections

## WHY THIS MATTERS
- Server has 2-3 minute timeout for operations
- Large writes exceed timeout and FAIL completely
- Chunked writes are FASTER and more RELIABLE
- Failed writes waste time and require retry

REMEMBER: When in doubt, write LESS per operation. Multiple small operations > one large operation.
`

// ResolveKiroModel extracts the base model identifier and agentic flag from model name.
func ResolveKiroModel(model string) (upstream string, agentic bool) {
	upstream = model
	if len(upstream) > len(KiroAgenticSuffix) && upstream[len(upstream)-len(KiroAgenticSuffix):] == KiroAgenticSuffix {
		agentic = true
		upstream = upstream[:len(upstream)-len(KiroAgenticSuffix)]
	}

	if len(upstream) > len(KiroThinkingSuffix) && upstream[len(upstream)-len(KiroThinkingSuffix):] == KiroThinkingSuffix {
		upstream = upstream[:len(upstream)-len(KiroThinkingSuffix)]
	}

	return
}

// ResolveKiroThinkingBudget resolves the thinking token budget from request body, headers, or model name.
func ResolveKiroThinkingBudget(body map[string]any, headers map[string]string, model string) *int {
	if re := extractThinkingFromBody(body); re != nil {
		return re
	}

	for k, v := range headers {
		if strings.EqualFold(k, "anthropic-beta") && strings.Contains(strings.ToLower(v), "interleaved-thinking") {
			budget := KiroThinkingBudgetDefault
			return &budget
		}
	}

	if containsThinkingModeTag(body) {
		budget := KiroThinkingBudgetDefault
		return &budget
	}

	if model != "" {
		m := strings.ToLower(model)
		if strings.Contains(m, "thinking") || strings.Contains(m, "-reason") {
			budget := KiroThinkingBudgetDefault
			return &budget
		}
	}

	return nil
}

func parseThinkingEffort(effort string) *int {
	level := strings.ToLower(effort)
	if level == "none" || level == "off" || level == "disabled" {
		return nil
	}

	budget := effortToBudget(level)
	if budget != nil {
		return budget
	}

	b := KiroThinkingBudgetDefault

	return &b
}

func extractEffortFromMap(m map[string]any, key string) *int {
	if sub, ok := m[key].(map[string]any); ok && sub != nil {
		if effort, okEff := sub["effort"].(string); okEff {
			return parseThinkingEffort(effort)
		}
	}

	return nil
}

func extractThinkingFromBody(body map[string]any) *int {
	if body == nil {
		return nil
	}

	if b := extractEffortFromMap(body, "output_config"); b != nil {
		return b
	}

	if re, ok := body["reasoning_effort"].(string); ok {
		return parseThinkingEffort(re)
	}

	if b := extractEffortFromMap(body, "reasoning"); b != nil {
		return b
	}

	if thinking, ok := body["thinking"].(map[string]any); ok && thinking != nil {
		if budgetTokens, okBT := thinking["budget_tokens"].(float64); okBT && budgetTokens > 0 {
			b := int(budgetTokens)
			return &b
		}
	}

	return nil
}

func effortToBudget(level string) *int {
	switch strings.ToLower(level) {
	case "low":
		b := 4000
		return &b
	case "medium":
		b := 10000
		return &b
	case "high":
		b := 16000
		return &b
	}

	return nil
}

func isThinkingMessage(msgRaw any) bool {
	msg, okMsg := msgRaw.(map[string]any)
	if !okMsg || msg == nil {
		return false
	}

	role, okRole := msg["role"].(string)
	if !okRole || (role != "system" && role != "user") {
		return false
	}

	return textContainsThinkingTag(msg["content"])
}

func containsThinkingModeTag(body map[string]any) bool {
	if body == nil {
		return false
	}

	if messages, ok := body["messages"].([]any); ok {
		for _, msgRaw := range messages {
			if isThinkingMessage(msgRaw) {
				return true
			}
		}
	}

	if sys, ok := body["system"].(string); ok {
		return textContainsThinkingTag(sys)
	}

	return false
}

func textContainsThinkingTag(content any) bool {
	var text string
	switch v := content.(type) {
	case string:
		text = v
	case []any:
		for _, p := range v {
			if block, ok := p.(map[string]any); ok && block != nil {
				if t, ok := block["text"].(string); ok {
					text += t
				}
			}
		}
	}

	if !strings.Contains(text, "<thinking_mode>") {
		return false
	}

	return strings.Contains(text, "<thinking_mode>enabled</thinking_mode>") ||
		strings.Contains(text, "<thinking_mode>interleaved</thinking_mode>")
}

// ResolveDefaultProfileArn resolves the default AWS profile ARN based on auth method.
func ResolveDefaultProfileArn(authMethod string) string {
	if authMethod == "google" || authMethod == "github" {
		return KiroDefaultProfileArnSocial
	}

	return KiroDefaultProfileArnBuilderID
}
