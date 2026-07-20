package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("vertex", &VertexExecutor{Base: Base{Provider: "vertex"}, partner: false})
	RegisterSpecialized("vertex-partner", &VertexExecutor{Base: Base{Provider: "vertex-partner"}, partner: true})
}

type VertexExecutor struct {
	Base
	partner bool
}

func (e *VertexExecutor) projectID(cred Credentials) string {
	// try parse SA JSON from apiKey
	if cred.APIKey != "" && strings.HasPrefix(strings.TrimSpace(cred.APIKey), "{") {
		var sa map[string]any
		if json.Unmarshal([]byte(cred.APIKey), &sa) == nil {
			if p, ok := sa["project_id"].(string); ok {
				return p
			}
			if p, ok := sa["quota_project_id"].(string); ok {
				return p
			}
		}
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
		if project == "" {
			project = "unknown"
		}
		url := fmt.Sprintf("https://aiplatform.googleapis.com/v1/projects/%s/locations/global/endpoints/openapi/chat/completions", project)
		if rawKey != "" {
			url += "?key=" + rawKey
		}
		return url
	}

	// Gemini via Vertex
	location := "us-central1"
	action := "generateContent"
	if stream {
		action = "streamGenerateContent?alt=sse"
	}
	if project == "" {
		// publishers path with API key
		url := fmt.Sprintf("https://aiplatform.googleapis.com/v1/publishers/google/models/%s:%s", model, action)
		if rawKey != "" {
			if strings.Contains(url, "?") {
				url += "&key=" + rawKey
			} else {
				url += "?key=" + rawKey
			}
		}
		return url
	}
	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:%s",
		location, project, location, model, action)
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
	// SA JSON is not a bearer token; AccessToken should be minted by tokenrefresh
	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	} else if cred.APIKey != "" && !strings.HasPrefix(strings.TrimSpace(cred.APIKey), "{") {
		// raw API key goes in query string
	}
	if stream {
		h.Set("Accept", "text/event-stream")
	}
	return h
}

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
