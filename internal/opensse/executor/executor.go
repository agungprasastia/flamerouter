package executor

import (
	"context"
	"io"
	"net/http"
)

type Credentials struct {
	APIKey               string
	AccessToken          string
	RefreshToken         string
	BaseURL              string
	ProjectID            string
	ProviderSpecificData map[string]any
}

type Result struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

type Executor interface {
	Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error)
}
