package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/shared/clineauth"
	"flamerouter/internal/translator/concerns"
	"fmt"
	"net/http"
	"strings"
)

const anthropicAPIVersionDefault = "2023-06-01"

type authSpec struct {
	Header string
	Scheme string // bearer | raw
}

type authDesc struct {
	APIKey           *authSpec
	OAuth            *authSpec
	Header           string
	Scheme           string
	Hooks            []string
	Combined         bool
	AnthropicVersion bool
}

var authDescriptors = map[string]authDesc{
	"claude":      {Combined: true, Header: "x-api-key", Scheme: "raw", AnthropicVersion: true, Hooks: []string{"claudeOverlay"}, APIKey: nil, OAuth: nil},
	"openai":      {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: nil, APIKey: nil, OAuth: nil},
	"openrouter":  {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: nil, APIKey: nil, OAuth: nil},
	"kimi":        {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: []string{"kimiHeaders"}, APIKey: nil, OAuth: nil},
	"kimi-coding": {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: []string{"kimiHeaders"}, APIKey: nil, OAuth: nil},
	"cline":       {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: []string{"clineHeaders"}, APIKey: nil, OAuth: nil},
	"clinepass":   {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: []string{"clineHeaders"}, APIKey: nil, OAuth: nil},
	"kilocode":    {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: []string{"kilocodeOrg"}, APIKey: nil, OAuth: nil},
	"deepseek":    {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: nil, APIKey: nil, OAuth: nil},
	"groq":        {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: nil, APIKey: nil, OAuth: nil},
	"mistral":     {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: nil, APIKey: nil, OAuth: nil},
	"together":    {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: nil, APIKey: nil, OAuth: nil},
	"fireworks":   {Combined: true, Header: "Authorization", Scheme: "bearer", AnthropicVersion: false, Hooks: nil, APIKey: nil, OAuth: nil},
}

// DefaultExecutor handles requests to generic OpenAI, Claude, or Gemini endpoints.
type DefaultExecutor struct {
	client    *http.Client
	headers   map[string]string
	quirks    map[string]bool
	provider  string
	format    string
	baseURL   string
	urlSuffix string
}

// NewDefault creates a new DefaultExecutor.
func NewDefault(c *http.Client) *DefaultExecutor {
	if c == nil {
		c = http.DefaultClient
	}

	return &DefaultExecutor{
		client:    c,
		headers:   nil,
		quirks:    nil,
		provider:  "",
		format:    "",
		baseURL:   "",
		urlSuffix: "",
	}
}

// NewDefaultForProvider creates a new DefaultExecutor configured for a given provider name.
func NewDefaultForProvider(c *http.Client, provider string) *DefaultExecutor {
	e := NewDefault(c)
	e.provider = provider

	switch {
	case provider == "claude" || strings.HasPrefix(provider, "anthropic-compatible"):
		e.format = "claude"
	case provider == "gemini" || strings.Contains(provider, "gemini"):
		e.format = "gemini"
	default:
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
			APIKey:           &authSpec{Header: "x-api-key", Scheme: "raw"},
			OAuth:            &authSpec{Header: "Authorization", Scheme: "bearer"},
			AnthropicVersion: true,
			Combined:         false,
			Header:           "",
			Scheme:           "",
			Hooks:            nil,
		}
	}

	if e.format == "claude" {
		return authDesc{
			Combined:         true,
			Header:           "x-api-key",
			Scheme:           "raw",
			AnthropicVersion: true,
			APIKey:           nil,
			OAuth:            nil,
			Hooks:            nil,
		}
	}

	return authDesc{
		Combined:         true,
		Header:           "Authorization",
		Scheme:           "bearer",
		AnthropicVersion: false,
		APIKey:           nil,
		OAuth:            nil,
		Hooks:            nil,
	}
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

	e.applyAuthToken(h, desc, cred)

	if desc.AnthropicVersion && h.Get("anthropic-version") == "" {
		h.Set("anthropic-version", anthropicAPIVersionDefault)
	}
}

func (e *DefaultExecutor) applyAuthToken(h http.Header, desc authDesc, cred Credentials) {
	switch {
	case desc.Combined:
		tok := cred.APIKey
		if tok == "" {
			tok = cred.AccessToken
		}

		header, scheme := desc.Header, desc.Scheme
		if header == "" {
			header, scheme = "Authorization", "bearer"
		}

		applyAuthHeader(h, header, scheme, tok)
	case cred.APIKey != "" && desc.APIKey != nil:
		applyAuthHeader(h, desc.APIKey.Header, desc.APIKey.Scheme, cred.APIKey)
	case cred.AccessToken != "" && desc.OAuth != nil:
		applyAuthHeader(h, desc.OAuth.Header, desc.OAuth.Scheme, cred.AccessToken)
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
		headers := clineauth.BuildClineHeaders(cred.APIKey, nil)
		if cred.APIKey == "" && cred.AccessToken != "" {
			headers = clineauth.BuildClineHeaders(cred.AccessToken, nil)
		}

		for k, v := range headers {
			h.Set(k, v)
		}
	case "kilocodeOrg":
		if org := strPSD(cred, "orgId"); org != "" {
			h.Set("X-Kilocode-OrganizationID", org)
		}
	case "claudeOverlay":
		// reserved for cached Claude Code identity headers
		_ = cred
	}
}

func (e *DefaultExecutor) buildCompatibleURL(cred Credentials) (string, bool) {
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
			return base + "/responses", true
		}

		return base + "/chat/completions", true
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

		return base + "/messages", true
	}

	return "", false
}

func (e *DefaultExecutor) buildFormatURL(model string, stream bool, cred Credentials) (string, bool) {
	if e.format == "gemini" {
		base := strings.TrimRight(cred.BaseURL, "/")
		if base == "" {
			base = "https://generativelanguage.googleapis.com/v1beta/models"
		}

		action := "generateContent"
		if stream {
			action = "streamGenerateContent?alt=sse"
		}

		return fmt.Sprintf("%s/%s:%s", base, model, action), true
	}

	if e.format == "claude" {
		base := strings.TrimRight(cred.BaseURL, "/")
		if base == "" {
			base = "https://api.anthropic.com/v1"
		}

		if !strings.HasSuffix(base, "/messages") {
			return base + "/messages", true
		}

		return base, true
	}

	return "", false
}

func getRuntimeTransportURL(cred Credentials) (string, bool) {
	if cred.ProviderSpecificData == nil {
		return "", false
	}

	rt, ok := cred.ProviderSpecificData["runtimeTransport"].(map[string]any)
	if !ok {
		return "", false
	}

	bu, ok := rt["baseUrl"].(string)
	if !ok || bu == "" {
		return "", false
	}

	if suf, ok := rt["urlSuffix"].(string); ok {
		return strings.TrimRight(bu, "/") + suf, true
	}

	return bu, true
}

func (e *DefaultExecutor) resolveBaseURL(cred Credentials) string {
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

func (e *DefaultExecutor) buildURL(model string, stream bool, cred Credentials) string {
	// runtime transport override
	if rtURL, ok := getRuntimeTransportURL(cred); ok {
		return rtURL
	}

	if url, ok := e.buildCompatibleURL(cred); ok {
		return url
	}

	if url, ok := e.buildFormatURL(model, stream, cred); ok {
		return url
	}

	return e.resolveBaseURL(cred)
}

func injectJSONSchemaFallback(body map[string]any) {
	rf, ok := body["response_format"].(map[string]any)
	if !ok {
		return
	}

	t, _ := rf["type"].(string) // nolint:errcheck
	if t != "json_schema" {
		return
	}

	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		return
	}

	schema, ok := js["schema"]
	if !ok {
		return
	}

	schemaJSON, _ := json.MarshalIndent(schema, "", "  ") // nolint:errcheck
	prompt := "You must respond with valid JSON that strictly follows this JSON schema:\n```json\n" + string(schemaJSON) + "\n```\nRespond ONLY with the JSON object, no other text."
	messages, _ := body["messages"].([]any) // nolint:errcheck
	found := false

	for _, msgRaw := range messages {
		msg, ok := msgRaw.(map[string]any)
		if !ok {
			continue
		}

		if role, _ := msg["role"].(string); role == "system" { // nolint:errcheck
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

func (e *DefaultExecutor) transform(model string, body map[string]any) map[string]any {
	if body == nil {
		return body
	}

	if e.quirks["dropClientMetadata"] || strings.HasPrefix(e.provider, "openai-compatible") {
		delete(body, "client_metadata")
	}
	// json_schema → json_object fallback for openai-compatible
	if strings.HasPrefix(e.provider, "openai-compatible-") {
		injectJSONSchemaFallback(body)
	}

	if e.provider != "" {
		concerns.StripUnsupportedParams(e.provider, model, body)
	}

	return body
}

func applyThirdPartyAnthropicHeaders(req *http.Header, cred Credentials) {
	if cred.APIKey != "" && req.Get("Authorization") == "" {
		req.Set("Authorization", "Bearer "+cred.APIKey)
	}

	req.Del("anthropic-dangerous-direct-browser-access")
	req.Del("x-app")

	if beta := req.Get("anthropic-beta"); beta != "" {
		var kept []string

		for _, f := range strings.Split(beta, ",") {
			f = strings.TrimSpace(f)
			if f != "" && f != "claude-code-20250219" {
				kept = append(kept, f)
			}
		}

		if len(kept) > 0 {
			req.Set("anthropic-beta", strings.Join(kept, ","))
		} else {
			req.Del("anthropic-beta")
		}
	}
}

func (e *DefaultExecutor) prepareRequest(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*http.Request, error) {
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
			applyThirdPartyAnthropicHeaders(&req.Header, cred)
		}
	}

	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	return req, nil
}

// Execute executes standard LLM requests across OpenAI/Claude/Gemini compatible formats.
func (e *DefaultExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	req, err := e.prepareRequest(ctx, cred, model, body, stream)
	if err != nil {
		return nil, err
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.Body == nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close() // nolint:errcheck
		}

		return nil, fmt.Errorf("nil response from upstream")
	}

	return &Result{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
}
