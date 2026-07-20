package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Base provides shared HTTP execute path for specialized executors.
type Base struct {
	Provider string
	Client   *http.Client
	Headers  map[string]string
	BaseURL  string
	BaseURLs []string
}

func (b *Base) client() *http.Client {
	if b.Client != nil {
		return b.Client
	}
	return http.DefaultClient
}

// BuildURL default: first base URL or credentials base.
func (b *Base) BuildURL(model string, stream bool, urlIndex int, cred Credentials) string {
	if len(b.BaseURLs) > 0 {
		if urlIndex < len(b.BaseURLs) {
			return b.BaseURLs[urlIndex]
		}
		return b.BaseURLs[0]
	}
	if b.BaseURL != "" {
		return b.BaseURL
	}
	return strings.TrimRight(cred.BaseURL, "/")
}

// BuildHeaders default Bearer auth.
func (b *Base) BuildHeaders(cred Credentials, stream bool) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	for k, v := range b.Headers {
		h.Set(k, v)
	}
	tok := cred.AccessToken
	if tok == "" {
		tok = cred.APIKey
	}
	if tok != "" {
		h.Set("Authorization", "Bearer "+tok)
	}
	if stream {
		h.Set("Accept", "text/event-stream")
	} else {
		h.Set("Accept", "application/json")
	}
	return h
}

// TransformRequest default no-op.
func (b *Base) TransformRequest(model string, body map[string]any, stream bool, cred Credentials) map[string]any {
	return body
}

// DoPOST executes POST with body.
func (b *Base) DoPOST(ctx context.Context, url string, headers http.Header, payload []byte) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	resp, err := b.client().Do(req)
	if err != nil {
		return nil, err
	}
	return &Result{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
}

// ExecuteJSON is the standard path: transform → marshal → post.
func (b *Base) ExecuteJSON(ctx context.Context, cred Credentials, model string, body []byte, stream bool,
	buildURL func(string, bool, int, Credentials) string,
	buildHeaders func(Credentials, bool) http.Header,
	transform func(string, map[string]any, bool, Credentials) map[string]any,
) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if transform != nil {
		m = transform(model, m, stream, cred)
	} else {
		m = b.TransformRequest(model, m, stream, cred)
	}
	m["model"] = model
	m["stream"] = stream
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	url := b.BuildURL(model, stream, 0, cred)
	if buildURL != nil {
		url = buildURL(model, stream, 0, cred)
	}
	headers := b.BuildHeaders(cred, stream)
	if buildHeaders != nil {
		headers = buildHeaders(cred, stream)
	}
	return b.DoPOST(ctx, url, headers, payload)
}

// DrainBody helper.
func DrainBody(r io.ReadCloser) {
	if r != nil {
		io.Copy(io.Discard, r)
		r.Close()
	}
}
