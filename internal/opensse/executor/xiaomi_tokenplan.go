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

// XiaomiTokenplanExecutor handles Xiaomi Tokenplan execution.
type XiaomiTokenplanExecutor struct {
	DefaultExecutor
}

// NewXiaomiTokenplanExecutor constructs a XiaomiTokenplanExecutor.
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

func isClaudeFormat(cred Credentials, model string) bool {
	if strings.HasSuffix(model, "-claude") {
		return true
	}

	if cred.ProviderSpecificData == nil {
		return false
	}

	if rt, ok := cred.ProviderSpecificData["runtimeTransport"].(map[string]any); ok {
		if fmtVal, okFmt := rt["format"].(string); okFmt && fmtVal == "claude" {
			return true
		}
	}

	if fmtVal, okFmt := cred.ProviderSpecificData["format"].(string); okFmt && fmtVal == "claude" {
		return true
	}

	return false
}

func buildXiaomiTokenplanURL(model string, _ bool, cred Credentials) string {
	baseURL := resolveXiaomiTokenplanBaseURL(cred)
	if strings.Contains(baseURL, "/anthropic/v1/messages") || strings.Contains(baseURL, "/chat/completions") {
		return baseURL
	}

	if isClaudeFormat(cred, model) {
		trimmed := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
		return trimmed + "/anthropic/v1/messages"
	}

	trimmed := strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(trimmed, "/chat/completions") {
		return trimmed
	}

	return trimmed + "/chat/completions"
}

// Execute executes Xiaomi Tokenplan requests.
func (e *XiaomiTokenplanExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	// Temporarily override or let DefaultExecutor build URL using custom logic
	url := buildXiaomiTokenplanURL(model, stream, cred)
	credCopy := cred
	credCopy.BaseURL = url

	return e.DefaultExecutor.Execute(ctx, credCopy, model, body, stream)
}
