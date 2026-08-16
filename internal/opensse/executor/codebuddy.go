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

var (
	codeBuddyAgentPattern = regexp.MustCompile(`(?i)you are claude code|claude.?code.+official.+cli|anthropic.+official.+cli|anxthxropic.+official.+cli|you are (?:cursor|windsurf|cline|aider|continue|copilot|cody)|you are an? (?:ai )?(?:coding |code )?agent|cc_entrypoint\s*=\s*(?:cli|vscode|jetbrains|gui)|claude.?code.+issues|give feedback.+claude.?code|you are .{0,30}(?:powerful )?ai agent|orchestration capabilities|OhMyOpenCode|<agent-identity>|<Role>|<Behavior_Instructions>`)
)

type CodeBuddyExecutor struct {
	DefaultExecutor
	providerID string
}

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

func transformCodeBuddyCN(body map[string]any) map[string]any {
	body["stream"] = true

	if rawMsgs, ok := body["messages"].([]any); ok {
		var newMsgs []any
		for _, item := range rawMsgs {
			msg, ok := item.(map[string]any)
			if !ok {
				newMsgs = append(newMsgs, item)
				continue
			}
			role, _ := msg["role"].(string)
			if role != "system" {
				newMsgs = append(newMsgs, msg)
				continue
			}

			text := flattenCodeBuddyContent(msg["content"])
			if text == "" {
				newMsgs = append(newMsgs, msg)
				continue
			}

			if len(text) > 2000 || codeBuddyAgentPattern.MatchString(text) {
				cloned := make(map[string]any, len(msg))
				for k, v := range msg {
					cloned[k] = v
				}
				if _, ok := msg["content"].(string); ok {
					cloned["content"] = codeBuddyNeutralPrompt
				} else {
					cloned["content"] = []any{map[string]any{"type": "text", "text": codeBuddyNeutralPrompt}}
				}
				newMsgs = append(newMsgs, cloned)
			} else {
				newMsgs = append(newMsgs, msg)
			}
		}
		body["messages"] = newMsgs
	}

	eff, hasEff := body["reasoning_effort"].(string)
	if hasEff {
		if eff == "none" || eff == "off" {
			delete(body, "reasoning_effort")
		} else if eff != "" {
			body["reasoning_summary"] = "auto"
		}
	}

	return body
}

func transformCodeBuddyIntl(body map[string]any) map[string]any {
	body["stream"] = true

	eff, hasEff := body["reasoning_effort"].(string)
	if hasEff {
		if eff == "none" || eff == "off" {
			delete(body, "reasoning_effort")
		} else if eff != "" {
			body["reasoning_summary"] = "auto"
		}
	}

	source, _ := body["messages"].([]any)
	newMessages := []any{
		map[string]any{"role": "system", "content": codeBuddyIntlSystem},
	}

	for _, item := range source {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "system" || role == "developer" {
			continue
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
				newMessages = append(newMessages, cloned)
				continue
			}
		}
		newMessages = append(newMessages, msg)
	}

	body["messages"] = newMessages
	return body
}

func (e *CodeBuddyExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
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
