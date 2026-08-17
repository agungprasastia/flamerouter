package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("vertex", &VertexExecutor{
		Base: Base{
			Provider: "vertex",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "",
			BaseURLs: nil,
		},
		partner: false,
	})
	RegisterSpecialized("vertex-partner", &VertexExecutor{
		Base: Base{
			Provider: "vertex-partner",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "",
			BaseURLs: nil,
		},
		partner: true,
	})
}

// VertexExecutor handles requests for Google Cloud Vertex AI and partner models.
type VertexExecutor struct {
	Base
	partner bool
}

func (e *VertexExecutor) projectID(cred Credentials) string {
	trimmed := strings.TrimSpace(cred.APIKey)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return ""
	}

	var sa map[string]any
	if err := json.Unmarshal([]byte(cred.APIKey), &sa); err != nil {
		return ""
	}

	if p, ok := sa["project_id"].(string); ok {
		return p
	}

	if p, ok := sa["quota_project_id"].(string); ok {
		return p
	}

	return ""
}

func (e *VertexExecutor) buildURL(model string, stream bool, cred Credentials) string {
	project := e.projectID(cred)

	rawKey := ""
	if !strings.HasPrefix(strings.TrimSpace(cred.APIKey), "{") && cred.AccessToken == "" {
		rawKey = cred.APIKey
	}

	if e.partner {
		return e.buildPartnerURL(project, rawKey)
	}

	return e.buildGeminiURL(model, stream, project, rawKey)
}

func (e *VertexExecutor) buildPartnerURL(project, rawKey string) string {
	if project == "" {
		project = "unknown"
	}

	url := fmt.Sprintf("https://aiplatform.googleapis.com/v1/projects/%s/locations/global/endpoints/openapi/chat/completions", project)
	if rawKey != "" {
		url += "?key=" + rawKey
	}

	return url
}

func (e *VertexExecutor) buildGeminiURL(model string, stream bool, project, rawKey string) string {
	location := "us-central1"

	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}

	var url string
	if project == "" {
		url = fmt.Sprintf("https://aiplatform.googleapis.com/v1/publishers/google/models/%s:%s", model, action)
	} else {
		url = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:%s",
			location, project, location, model, action)
	}

	if rawKey != "" {
		if strings.Contains(url, "?") {
			url += "&key=" + rawKey
		} else {
			url += "?key=" + rawKey
		}
	}

	return url
}

func (e *VertexExecutor) buildHeaders(cred Credentials, stream bool) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")

	tok := cred.AccessToken
	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}

	if stream {
		h.Set("Accept", "text/event-stream")
	}

	return h
}

// Execute executes Vertex AI requests.
func (e *VertexExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	if e.partner {
		m["model"] = model
		m["stream"] = stream
	}

	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	url := e.buildURL(model, stream, cred)
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" && strings.Contains(base, "aiplatform") {
		url = base
	}

	return e.DoPOST(ctx, url, e.buildHeaders(cred, stream), payload)
}
