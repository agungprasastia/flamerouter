package executor

import (
	"context"
	"io"
	"net/http"
)

// Credentials holds authentication information for model providers.
type Credentials struct {
	ProviderSpecificData map[string]any
	APIKey               string
	AccessToken          string
	RefreshToken         string
	BaseURL              string
	ProjectID            string
}

// Result holds upstream HTTP execution response data.
type Result struct {
	Body       io.ReadCloser
	Header     http.Header
	StatusCode int
}

// Executor executes LLM upstream requests.
type Executor interface {
	Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error)
}
