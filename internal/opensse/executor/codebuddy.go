package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

func init() {
	RegisterSpecialized("codebuddy-cn", NewCodeBuddyExecutor("codebuddy-cn", nil))
	RegisterSpecialized("codebuddy-intl", NewCodeBuddyExecutor("codebuddy-intl", nil))
}

const (
	codeBuddyNeutralPrompt = "You are a helpful AI assistant that helps with software engineering tasks."
	codeBuddyIntlSystem    = "You are CodeBuddy Code."
)

var codeBuddyAgentPattern = regexp.MustCompile(`(?i)you are claude code|claude.?code.+official.+cli|anthropic.+official.+cli|anxthxropic.+official.+cli|you are (?:cursor|windsurf|cline|aider|continue|copilot|cody)|you are an? (?:ai )?(?:coding |code )?agent|cc_entrypoint\s*=\s*(?:cli|vscode|jetbrains|gui)|claude.?code.+issues|give feedback.+claude.?code|you are .{0,30}(?:powerful )?ai agent|orchestration capabilities|OhMyOpenCode|<agent-identity>|<Role>|<Behavior_Instructions>`)

// CodeBuddyExecutor handles CodeBuddy CN and International providers.
type CodeBuddyExecutor struct {
	DefaultExecutor
	providerID string
}

// NewCodeBuddyExecutor creates a new CodeBuddyExecutor.
func NewCodeBuddyExecutor(providerID string, client *http.Client) *CodeBuddyExecutor {
	if client == nil {
		client = http.DefaultClient
	}

	e := NewDefaultForProvider(client, providerID)
	if providerID == "codebuddy-intl" {
		e.baseURL = "https://www.codebuddy.ai/v2"
	} else {
		e.baseURL = "https://copilot.tencent.com/v2"
	}

	return &CodeBuddyExecutor{
		DefaultExecutor: *e,
		providerID:      providerID,
	}
}

func flattenCodeBuddyContent(content any) string {
	if content == nil {
		return ""
	}

	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string

		for _, b := range v {
			if m, ok := b.(map[string]any); ok {
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
		}

		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(v)
	}
}

func sanitizeCodeBuddySystemMsg(msg map[string]any) map[string]any {
	text := flattenCodeBuddyContent(msg["content"])
	if text == "" {
		return msg
	}

	if len(text) <= 2000 && !codeBuddyAgentPattern.MatchString(text) {
		return msg
	}

	cloned := make(map[string]any, len(msg))
	for k, v := range msg {
		cloned[k] = v
	}

	if _, ok := msg["content"].(string); ok {
		cloned["content"] = codeBuddyNeutralPrompt
	} else {
		cloned["content"] = []any{map[string]any{"type": "text", "text": codeBuddyNeutralPrompt}}
	}

	return cloned
}

func sanitizeCodeBuddyCNMessages(rawMsgs []any) []any {
	newMsgs := make([]any, 0, len(rawMsgs))

	for _, item := range rawMsgs {
		msg, ok := item.(map[string]any)
		if !ok {
			newMsgs = append(newMsgs, item)
			continue
		}

		role, _ := msg["role"].(string) // nolint:errcheck
		if role != "system" {
			newMsgs = append(newMsgs, msg)
			continue
		}

		newMsgs = append(newMsgs, sanitizeCodeBuddySystemMsg(msg))
	}

	return newMsgs
}

func applyCodeBuddyReasoning(body map[string]any) {
	eff, hasEff := body["reasoning_effort"].(string)
	if !hasEff {
		return
	}

	effLower := strings.ToLower(eff)
	if effLower == "none" || effLower == "off" {
		delete(body, "reasoning_effort")
	} else if eff != "" {
		body["reasoning_summary"] = "auto"
	}
}

func transformCodeBuddyCN(body map[string]any) map[string]any {
	body["stream"] = true

	if rawMsgs, ok := body["messages"].([]any); ok {
		body["messages"] = sanitizeCodeBuddyCNMessages(rawMsgs)
	}

	applyCodeBuddyReasoning(body)

	return body
}

func sanitizeCodeBuddyIntlItem(item any) (map[string]any, bool) {
	msg, ok := item.(map[string]any)
	if !ok {
		return nil, false
	}

	role, _ := msg["role"].(string) // nolint:errcheck
	if role == "system" || role == "developer" {
		return nil, false
	}

	if role == "user" {
		if contentStr, ok := msg["content"].(string); ok {
			cloned := make(map[string]any, len(msg))
			for k, v := range msg {
				cloned[k] = v
			}

			cloned["content"] = []any{
				map[string]any{"type": "text", "text": contentStr},
			}

			return cloned, true
		}
	}

	return msg, true
}

func transformCodeBuddyIntl(body map[string]any) map[string]any {
	body["stream"] = true

	applyCodeBuddyReasoning(body)

	source, _ := body["messages"].([]any) // nolint:errcheck
	newMessages := []any{
		map[string]any{"role": "system", "content": codeBuddyIntlSystem},
	}

	for _, item := range source {
		if transformed, ok := sanitizeCodeBuddyIntlItem(item); ok {
			newMessages = append(newMessages, transformed)
		}
	}

	body["messages"] = newMessages

	return body
}

// Execute executes CodeBuddy requests.
func (e *CodeBuddyExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, _ bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	if e.providerID == "codebuddy-intl" {
		m = transformCodeBuddyIntl(m)
	} else {
		m = transformCodeBuddyCN(m)
	}

	newBody, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	return e.DefaultExecutor.Execute(ctx, cred, model, newBody, true)
}
