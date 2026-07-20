package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"flamerouter/internal/translator/concerns"
)

const anthropicAPIVersionDefault = "2023-06-01"

type authSpec struct {
	Header string
	Scheme string // bearer | raw
}

type authDesc struct {
	Combined         bool
	Header           string
	Scheme           string
	APIKey           *authSpec
	OAuth            *authSpec
	AnthropicVersion bool
	Hooks            []string
}

var authDescriptors = map[string]authDesc{
	"claude":  {Combined: true, Header: "x-api-key", Scheme: "raw", AnthropicVersion: true, Hooks: []string{"claudeOverlay"}},
	"openai":  {Combined: true, Header: "Authorization", Scheme: "bearer"},
	"openrouter": {Combined: true, Header: "Authorization", Scheme: "bearer"},
	"kimi":    {Combined: true, Header: "Authorization", Scheme: "bearer", Hooks: []string{"kimiHeaders"}},
	"kimi-coding": {Combined: true, Header: "Authorization", Scheme: "bearer", Hooks: []string{"kimiHeaders"}},
	"cline":   {Combined: true, Header: "Authorization", Scheme: "bearer", Hooks: []string{"clineHeaders"}},
	"clinepass": {Combined: true, Header: "Authorization", Scheme: "bearer", Hooks: []string{"clineHeaders"}},
	"kilocode": {Combined: true, Header: "Authorization", Scheme: "bearer", Hooks: []string{"kilocodeOrg"}},
	"deepseek": {Combined: true, Header: "Authorization", Scheme: "bearer"},
	"groq":    {Combined: true, Header: "Authorization", Scheme: "bearer"},
	"mistral": {Combined: true, Header: "Authorization", Scheme: "bearer"},
	"together": {Combined: true, Header: "Authorization", Scheme: "bearer"},
	"fireworks": {Combined: true, Header: "Authorization", Scheme: "bearer"},
}

type DefaultExecutor struct {
	client   *http.Client
	provider string
	format   string // openai | claude | gemini
	baseURL  string
	urlSuffix string
	headers  map[string]string
	quirks   map[string]bool
}

func NewDefault(c *http.Client) *DefaultExecutor {
	if c == nil {
		c = http.DefaultClient
	}
	return &DefaultExecutor{client: c}
}

func NewDefaultForProvider(c *http.Client, provider string) *DefaultExecutor {
	e := NewDefault(c)
	e.provider = provider
	// infer format from name
	if provider == "claude" || strings.HasPrefix(provider, "anthropic-compatible") {
		e.format = "claude"
	} else if provider == "gemini" || strings.Contains(provider, "gemini") {
		e.format = "gemini"
	} else {
		e.format = "openai"
	}
	return e
}

func (e *DefaultExecutor) resolveAuthDesc() authDesc {
	if d, ok := authDescriptors[e.provider]; ok {
		return d
	}
	if strings.HasPrefix(e.provider, "anthropic-compatible-") {
		return authDesc{
			APIKey: &authSpec{Header: "x-api-key", Scheme: "raw"},
			OAuth:  &authSpec{Header: "Authorization", Scheme: "bearer"},
			AnthropicVersion: true,
		}
	}
	if e.format == "claude" {
		return authDesc{Combined: true, Header: "x-api-key", Scheme: "raw", AnthropicVersion: true}
	}
	return authDesc{Combined: true, Header: "Authorization", Scheme: "bearer"}
}

func applyAuthHeader(h http.Header, header, scheme, token string) {
	if scheme == "bearer" {
		h.Set(header, "Bearer "+token)
	} else {
		h.Set(header, token)
	}
}

func (e *DefaultExecutor) applyAuth(h http.Header, desc authDesc, cred Credentials) {
	for _, hook := range desc.Hooks {
		applyHeaderHook(h, hook, cred)
	}
	if desc.Combined {
		tok := cred.APIKey
		if tok == "" {
			tok = cred.AccessToken
		}
		header, scheme := desc.Header, desc.Scheme
		if header == "" {
			header, scheme = "Authorization", "bearer"
		}
		applyAuthHeader(h, header, scheme, tok)
	} else {
		if cred.APIKey != "" && desc.APIKey != nil {
			applyAuthHeader(h, desc.APIKey.Header, desc.APIKey.Scheme, cred.APIKey)
		} else if cred.AccessToken != "" && desc.OAuth != nil {
			applyAuthHeader(h, desc.OAuth.Header, desc.OAuth.Scheme, cred.AccessToken)
		}
	}
	if desc.AnthropicVersion && h.Get("anthropic-version") == "" {
		h.Set("anthropic-version", anthropicAPIVersionDefault)
	}
}

func applyHeaderHook(h http.Header, hook string, cred Credentials) {
	switch hook {
	case "kimiHeaders":
		h.Set("User-Agent", "Kimi/1.0")
		if did := strPSD(cred, "deviceId"); did != "" {
			h.Set("X-Msh-Device-Id", did)
		}
	case "clineHeaders":
		h.Set("User-Agent", "Cline/1.0")
	case "kilocodeOrg":
		if org := strPSD(cred, "orgId"); org != "" {
			h.Set("X-Kilocode-OrganizationID", org)
		}
	case "claudeOverlay":
		// reserved for cached Claude Code identity headers
	}
}

func (e *DefaultExecutor) buildURL(model string, stream bool, cred Credentials) string {
	// runtime transport override
	if cred.ProviderSpecificData != nil {
		if rt, ok := cred.ProviderSpecificData["runtimeTransport"].(map[string]any); ok {
			if bu, ok := rt["baseUrl"].(string); ok && bu != "" {
				if suf, ok := rt["urlSuffix"].(string); ok {
					return strings.TrimRight(bu, "/") + suf
				}
				return bu
			}
		}
	}
	if strings.HasPrefix(e.provider, "openai-compatible-") {
		base := strPSD(cred, "baseUrl")
		if base == "" {
			base = cred.BaseURL
		}
		if base == "" {
			base = "https://api.openai.com/v1"
		}
		base = strings.TrimRight(base, "/")
		if strings.Contains(e.provider, "responses") {
			return base + "/responses"
		}
		return base + "/chat/completions"
	}
	if strings.HasPrefix(e.provider, "anthropic-compatible-") {
		base := strPSD(cred, "baseUrl")
		if base == "" {
			base = cred.BaseURL
		}
		if base == "" {
			base = "https://api.anthropic.com/v1"
		}
		base = strings.TrimRight(base, "/")
		return base + "/messages"
	}
	if e.format == "gemini" {
		base := strings.TrimRight(cred.BaseURL, "/")
		if base == "" {
			base = "https://generativelanguage.googleapis.com/v1beta/models"
		}
		action := "generateContent"
		if stream {
			action = "streamGenerateContent?alt=sse"
		}
		return fmt.Sprintf("%s/%s:%s", base, model, action)
	}
	if e.format == "claude" {
		base := strings.TrimRight(cred.BaseURL, "/")
		if base == "" {
			base = "https://api.anthropic.com/v1"
		}
		if !strings.HasSuffix(base, "/messages") {
			return base + "/messages"
		}
		return base
	}
	base := strings.TrimRight(cred.BaseURL, "/")
	if base == "" && e.baseURL != "" {
		base = strings.TrimRight(e.baseURL, "/")
	}
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if strings.Contains(base, "{accountId}") {
		aid := strPSD(cred, "accountId")
		base = strings.ReplaceAll(base, "{accountId}", aid)
	}
	if e.urlSuffix != "" {
		return base + e.urlSuffix
	}
	if strings.HasSuffix(base, "/chat/completions") || strings.HasSuffix(base, "/messages") {
		return base
	}
	return base + "/chat/completions"
}

func (e *DefaultExecutor) transform(model string, body map[string]any) map[string]any {
	if body == nil {
		return body
	}
	if e.quirks["dropClientMetadata"] || strings.HasPrefix(e.provider, "openai-compatible") {
		delete(body, "client_metadata")
	}
	// json_schema → json_object fallback for openai-compatible
	if strings.HasPrefix(e.provider, "openai-compatible-") {
		if rf, ok := body["response_format"].(map[string]any); ok {
			if t, _ := rf["type"].(string); t == "json_schema" {
				if js, ok := rf["json_schema"].(map[string]any); ok {
					if schema, ok := js["schema"]; ok {
						schemaJSON, _ := json.MarshalIndent(schema, "", "  ")
						prompt := "You must respond with valid JSON that strictly follows this JSON schema:\n```json\n" + string(schemaJSON) + "\n```\nRespond ONLY with the JSON object, no other text."
						messages, _ := body["messages"].([]any)
						found := false
						for _, msgRaw := range messages {
							msg, ok := msgRaw.(map[string]any)
							if !ok {
								continue
							}
							if role, _ := msg["role"].(string); role == "system" {
								if s, ok := msg["content"].(string); ok {
									msg["content"] = s + "\n\n" + prompt
									found = true
									break
								}
							}
						}
						if !found {
							messages = append([]any{map[string]any{"role": "system", "content": prompt}}, messages...)
						}
						body["messages"] = messages
						body["response_format"] = map[string]any{"type": "json_object"}
					}
				}
			}
		}
	}
	if e.provider != "" {
		concerns.StripUnsupportedParams(e.provider, model, body)
	}
	return body
}

func (e *DefaultExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	m["model"] = model
	m["stream"] = stream
	m = e.transform(model, m)
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	url := e.buildURL(model, stream, cred)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}
	desc := e.resolveAuthDesc()
	e.applyAuth(req.Header, desc, cred)

	// third-party anthropic-compatible: dual auth + strip CC identity
	if strings.HasPrefix(e.provider, "anthropic-compatible-") {
		baseURL := strPSD(cred, "baseUrl")
		if baseURL == "" {
			baseURL = cred.BaseURL
		}
		isOfficial := baseURL == "" || strings.Contains(baseURL, "api.anthropic.com")
		if !isOfficial {
			if cred.APIKey != "" && req.Header.Get("Authorization") == "" {
				req.Header.Set("Authorization", "Bearer "+cred.APIKey)
			}
			req.Header.Del("anthropic-dangerous-direct-browser-access")
			req.Header.Del("x-app")
			if beta := req.Header.Get("anthropic-beta"); beta != "" {
				var kept []string
				for _, f := range strings.Split(beta, ",") {
					f = strings.TrimSpace(f)
					if f != "" && f != "claude-code-20250219" {
						kept = append(kept, f)
					}
				}
				if len(kept) > 0 {
					req.Header.Set("anthropic-beta", strings.Join(kept, ","))
				} else {
					req.Header.Del("anthropic-beta")
				}
			}
		}
	}

	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	return &Result{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
}
