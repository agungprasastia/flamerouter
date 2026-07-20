package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func init() {
	RegisterSpecialized("azure", &AzureExecutor{Base: Base{Provider: "azure"}})
}

type AzureExecutor struct{ Base }

func (e *AzureExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	m["stream"] = stream

	endpoint := strPSD(cred, "azureEndpoint")
	if endpoint == "" {
		endpoint = envOr("AZURE_ENDPOINT", "https://api.openai.com")
	}
	apiVersion := strPSD(cred, "apiVersion")
	if apiVersion == "" {
		apiVersion = envOr("AZURE_API_VERSION", "2024-10-01-preview")
	}
	deployment := strPSD(cred, "deployment")
	if deployment == "" {
		deployment = model
	}
	if deployment == "" {
		deployment = envOr("AZURE_DEPLOYMENT", "gpt-4")
	}
	endpoint = strings.TrimRight(endpoint, "/")
	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s", endpoint, deployment, apiVersion)

	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	apiKey := cred.APIKey
	if apiKey == "" {
		apiKey = cred.AccessToken
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey != "" {
		h.Set("api-key", apiKey)
	}
	if org := strPSD(cred, "organization"); org != "" {
		h.Set("OpenAI-Organization", org)
	}
	if stream {
		h.Set("Accept", "text/event-stream")
	}
	return e.DoPOST(ctx, url, h, payload)
}

func strPSD(cred Credentials, key string) string {
	if cred.ProviderSpecificData == nil {
		return ""
	}
	if v, ok := cred.ProviderSpecificData[key].(string); ok {
		return v
	}
	return ""
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
