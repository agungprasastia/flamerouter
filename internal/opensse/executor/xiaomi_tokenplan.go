package executor

import (
	"context"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("xiaomi-tokenplan", NewXiaomiTokenplanExecutor(nil))
}

var xiaomiTokenplanRegions = map[string]string{
	"sgp": "https://token-plan-sgp.xiaomimimo.com/v1",
	"cn":  "https://token-plan-cn.xiaomimimo.com/v1",
	"ams": "https://token-plan-ams.xiaomimimo.com/v1",
}

const xiaomiTokenplanDefaultRegion = "sgp"

type XiaomiTokenplanExecutor struct {
	DefaultExecutor
}

func NewXiaomiTokenplanExecutor(client *http.Client) *XiaomiTokenplanExecutor {
	if client == nil {
		client = http.DefaultClient
	}
	e := NewDefaultForProvider(client, "xiaomi-tokenplan")
	return &XiaomiTokenplanExecutor{
		DefaultExecutor: *e,
	}
}

func resolveXiaomiTokenplanBaseURL(cred Credentials) string {
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		return base
	}
	region := strPSD(cred, "region")
	if rURL, ok := xiaomiTokenplanRegions[region]; ok {
		return rURL
	}
	return xiaomiTokenplanRegions[xiaomiTokenplanDefaultRegion]
}

func buildXiaomiTokenplanURL(model string, stream bool, cred Credentials) string {
	baseURL := resolveXiaomiTokenplanBaseURL(cred)
	if strings.Contains(baseURL, "/anthropic/v1/messages") || strings.Contains(baseURL, "/chat/completions") {
		return baseURL
	}
	isClaude := false

	if cred.ProviderSpecificData != nil {
		if rt, ok := cred.ProviderSpecificData["runtimeTransport"].(map[string]any); ok {
			if fmt, ok := rt["format"].(string); ok && fmt == "claude" {
				isClaude = true
			}
		}
		if fmt, ok := cred.ProviderSpecificData["format"].(string); ok && fmt == "claude" {
			isClaude = true
		}
	}
	if strings.HasSuffix(model, "-claude") {
		isClaude = true
	}

	if isClaude {
		trimmed := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
		return trimmed + "/anthropic/v1/messages"
	}

	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/chat/completions") {
		return trimmed
	}
	return trimmed + "/chat/completions"
}

func (e *XiaomiTokenplanExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	// Temporarily override or let DefaultExecutor build URL using custom logic
	url := buildXiaomiTokenplanURL(model, stream, cred)
	credCopy := cred
	credCopy.BaseURL = url
	return e.DefaultExecutor.Execute(ctx, credCopy, model, body, stream)
}
