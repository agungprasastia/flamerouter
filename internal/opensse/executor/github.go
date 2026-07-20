package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"flamerouter/internal/translator/concerns"
)

const (
	ghVSCodeVersion      = "1.98.2"
	ghCopilotChatVersion = "0.25.1"
	ghUserAgent          = "GitHubCopilotChat/" + ghCopilotChatVersion
	ghAPIVersion         = "2025-04-01"
	anthropicAPIVersion  = "2023-06-01"
)

func init() {
	RegisterSpecialized("github", &GithubExecutor{Base: Base{Provider: "github", BaseURL: "https://api.githubcopilot.com/chat/completions"}})
	RegisterSpecialized("copilot", &GithubExecutor{Base: Base{Provider: "github", BaseURL: "https://api.githubcopilot.com/chat/completions"}})
}

type GithubExecutor struct {
	Base
}

func (e *GithubExecutor) isClaudeModel(model string) bool {
	return regexp.MustCompile(`(?i)claude`).MatchString(model)
}

func (e *GithubExecutor) requiresMaxCompletionTokens(model string) bool {
	return regexp.MustCompile(`(?i)gpt-5|o[134]-`).MatchString(model)
}

func (e *GithubExecutor) buildHeaders(cred Credentials, stream bool) map[string][]string {
	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}
	rid := randomHex(16)
	h := map[string][]string{
		"Authorization":                     {fmt.Sprintf("Bearer %s", tok)},
		"Content-Type":                      {"application/json"},
		"copilot-integration-id":            {"vscode-chat"},
		"editor-version":                    {fmt.Sprintf("vscode/%s", ghVSCodeVersion)},
		"editor-plugin-version":             {fmt.Sprintf("copilot-chat/%s", ghCopilotChatVersion)},
		"user-agent":                        {ghUserAgent},
		"openai-intent":                     {"conversation-panel"},
		"x-github-api-version":              {ghAPIVersion},
		"x-request-id":                      {rid},
		"x-vscode-user-agent-library-version": {"electron-fetch"},
		"X-Initiator":                       {"user"},
		"anthropic-version":                 {anthropicAPIVersion},
	}
	if stream {
		h["Accept"] = []string{"text/event-stream"}
	} else {
		h["Accept"] = []string{"application/json"}
	}
	return h
}

func (e *GithubExecutor) sanitizeMessages(body map[string]any) {
	messages, ok := body["messages"].([]any)
	if !ok {
		return
	}
	for i, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		var clean []any
		for _, partRaw := range content {
			part, ok := partRaw.(map[string]any)
			if !ok {
				continue
			}
			t, _ := part["type"].(string)
			if t == "text" || t == "image_url" {
				clean = append(clean, part)
				continue
			}
			text := ""
			if s, ok := part["text"].(string); ok {
				text = s
			} else if s, ok := part["content"].(string); ok {
				text = s
			} else {
				b, _ := json.Marshal(part)
				text = string(b)
			}
			if text != "" {
				clean = append(clean, map[string]any{"type": "text", "text": text})
			}
		}
		if len(clean) > 0 {
			msg["content"] = clean
		} else {
			msg["content"] = nil
		}
		messages[i] = msg
	}
}

func (e *GithubExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	m["model"] = model
	m["stream"] = stream
	concerns.StripUnsupportedParams("github", model, m)

	if e.requiresMaxCompletionTokens(model) {
		if mt, ok := m["max_tokens"]; ok {
			m["max_completion_tokens"] = mt
			delete(m, "max_tokens")
		}
	}

	url := "https://api.githubcopilot.com/chat/completions"
	if e.isClaudeModel(model) {
		// Anthropic-native messages shim for cache token counts
		url = "https://api.githubcopilot.com/v1/messages"
	} else {
		e.sanitizeMessages(m)
	}

	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		// allow override but keep path
		if !strings.Contains(base, "githubcopilot") {
			url = base + "/chat/completions"
		}
	}

	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	headers := e.buildHeaders(cred, stream)
	h := make(map[string][]string)
	for k, v := range headers {
		h[k] = v
	}
	// convert to http.Header via DoPOST
	hh := e.Base.BuildHeaders(cred, stream)
	for k, vals := range headers {
		hh.Del(k)
		for _, v := range vals {
			hh.Add(k, v)
		}
	}
	return e.DoPOST(ctx, url, hh, payload)
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
