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
	RegisterSpecialized("kimchi", NewKimchiExecutor(nil))
}

const reasoningPlaceholderMaxLen = 8

var (
	topLevelOpenAIGatewayDrops = []string{
		"anthropic_version",
		"anthropic_beta",
		"client_metadata",
		"mcp_servers",
		"stop_sequences",
		"thinking",
		"top_k",
	}
	anthropicKimchiRe = regexp.MustCompile(`(?i)(?:^|[-_/])(?:claude|anthropic)(?:[-_/]|$)`)
)

type KimchiExecutor struct {
	DefaultExecutor
}

func NewKimchiExecutor(client *http.Client) *KimchiExecutor {
	if client == nil {
		client = http.DefaultClient
	}

	e := NewDefaultForProvider(client, "kimchi")

	return &KimchiExecutor{
		DefaultExecutor: *e,
	}
}

func kimchiSystemToText(system any) string {
	if system == nil {
		return ""
	}

	switch s := system.(type) {
	case string:
		return s
	case []any:
		var parts []string

		for _, part := range s {
			if str, ok := part.(string); ok && str != "" {
				parts = append(parts, str)
			} else if m, ok := part.(map[string]any); ok {
				if t, ok := m["text"].(string); ok && t != "" {
					parts = append(parts, t)
				}
			}
		}

		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(s)
	}
}

func mergeKimchiTopLevelSystem(body map[string]any) {
	sysVal, hasSys := body["system"]
	if !hasSys || sysVal == nil {
		return
	}

	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}

	text := strings.TrimSpace(kimchiSystemToText(sysVal))
	if text == "" {
		return
	}

	var existingSystem map[string]any

	for _, item := range messages {
		if msg, ok := item.(map[string]any); ok {
			if role, _ := msg["role"].(string); role == "system" {
				existingSystem = msg
				break
			}
		}
	}

	if existingSystem == nil {
		body["messages"] = append([]any{map[string]any{"role": "system", "content": text}}, messages...)
		return
	}

	switch c := existingSystem["content"].(type) {
	case string:
		existingSystem["content"] = fmt.Sprintf("%s\n\n%s", text, c)
	case []any:
		existingSystem["content"] = append([]any{map[string]any{"type": "text", "text": text}}, c...)
	}
}

func stripKimchiMessageArtifacts(body map[string]any) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}

	for _, item := range messages {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}

		delete(msg, "cache_control")

		if contentArr, ok := msg["content"].([]any); ok {
			cleaned := make([]any, 0, len(contentArr))

			for _, part := range contentArr {
				partMap, ok := part.(map[string]any)
				if !ok {
					cleaned = append(cleaned, part)
					continue
				}

				newPart := make(map[string]any, len(partMap))

				for k, v := range partMap {
					if k != "cache_control" && k != "signature" {
						newPart[k] = v
					}
				}

				cleaned = append(cleaned, newPart)
			}

			msg["content"] = cleaned
		}
	}
}

func stripKimchiToolArtifacts(body map[string]any) {
	tools, ok := body["tools"].([]any)
	if !ok {
		return
	}

	cleaned := make([]any, 0, len(tools))

	for _, item := range tools {
		toolMap, ok := item.(map[string]any)
		if !ok {
			cleaned = append(cleaned, item)
			continue
		}

		newTool := make(map[string]any, len(toolMap))

		for k, v := range toolMap {
			if k != "cache_control" {
				newTool[k] = v
			}
		}

		cleaned = append(cleaned, newTool)
	}

	body["tools"] = cleaned
}

func stripKimchiReasoningContent(body map[string]any) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}

	for _, item := range messages {
		if msg, ok := item.(map[string]any); ok {
			role, _ := msg["role"].(string)
			if role == "assistant" {
				if rc, ok := msg["reasoning_content"].(string); ok && len(rc) > reasoningPlaceholderMaxLen {
					delete(msg, "reasoning_content")
				}
			}
		}
	}
}

func isAnthropicBackedKimchiModel(model string) bool {
	return anthropicKimchiRe.MatchString(model)
}

func transformKimchiRequest(model string, body map[string]any) map[string]any {
	if body == nil {
		return body
	}

	mergeKimchiTopLevelSystem(body)

	for _, key := range topLevelOpenAIGatewayDrops {
		delete(body, key)
	}

	delete(body, "system")

	if isAnthropicBackedKimchiModel(model) {
		delete(body, "reasoning_effort")
		delete(body, "reasoning")
		delete(body, "thinking")
	}

	stripKimchiMessageArtifacts(body)
	stripKimchiToolArtifacts(body)
	stripKimchiReasoningContent(body)

	return body
}

func (e *KimchiExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	m = transformKimchiRequest(model, m)

	newBody, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	return e.DefaultExecutor.Execute(ctx, cred, model, newBody, stream)
}
