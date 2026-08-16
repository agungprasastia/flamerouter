package executor

import (
	"context"
	"io"
	"net/http"
)

type Credentials struct {
	ProviderSpecificData map[string]any
	APIKey               string
	AccessToken          string
	RefreshToken         string
	BaseURL              string
	ProjectID            string
}

type Result struct {
	Body       io.ReadCloser
	Header     http.Header
	StatusCode int
}

type Executor interface {
	Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error)
}
