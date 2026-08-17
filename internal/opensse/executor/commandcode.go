package executor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("commandcode", &CommandCodeExecutor{
		Base: Base{
			Provider: "commandcode",
			Client:   nil,
			Headers:  nil,
			BaseURL:  "https://api.commandcode.ai/alpha/generate",
			BaseURLs: nil,
		},
	})
}

// CommandCodeExecutor executes CommandCode streaming requests.
type CommandCodeExecutor struct {
	Base
}

func resolveCommandCodeURL(baseURL, credBase string) string {
	if credBase == "" {
		return baseURL
	}

	if !strings.Contains(credBase, "/alpha/") {
		return credBase + "/alpha/generate"
	}

	return credBase
}

func buildCommandCodeRequest(ctx context.Context, url string, payload []byte, cred Credentials) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("x-session-id", randomUUIDSimple())

	tok := cred.APIKey
	if tok == "" {
		tok = cred.AccessToken
	}

	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	return req, nil
}

func processCommandCodeStream(r io.ReadCloser, pw *io.PipeWriter) {
	defer func() {
		if err := pw.Close(); err != nil {
			_ = err
		}

		if err := r.Close(); err != nil {
			_ = err
		}
	}()

	state := concerns.NewResponseState()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw map[string]any
		if json.Unmarshal([]byte(line), &raw) == nil {
			chunks := translator.DefaultRegistry.TranslateResponse(
				translator.FormatCommandCode, translator.FormatOpenAI, raw, state,
			)
			for _, c := range chunks {
				if j, err := json.Marshal(c); err == nil {
					if _, err := pw.Write([]byte("data: " + string(j) + "\n\n")); err != nil {
						_ = err
					}
				}
			}
		}
	}

	if _, err := pw.Write([]byte("data: [DONE]\n\n")); err != nil {
		_ = err
	}
}

func prepareCommandCodePayload(model string, body []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	m["stream"] = true // always stream NDJSON upstream
	m["model"] = model

	return json.Marshal(m)
}

// Execute executes CommandCode requests.
func (e *CommandCodeExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, _ bool) (*Result, error) {
	payload, err := prepareCommandCodePayload(model, body)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(cred.BaseURL, "/")
	url := resolveCommandCodeURL(e.BaseURL, base)

	req, err := buildCommandCodeRequest(ctx, url, payload, cred)
	if err != nil {
		return nil, err
	}

	resp, err := e.client().Do(req)
	if err != nil {
		return nil, err
	}

	if resp == nil || resp.Body == nil {
		if resp != nil && resp.Body != nil {
			errClose := resp.Body.Close()
			if errClose != nil {
				return nil, fmt.Errorf("closing response body: %w", errClose)
			}
		}

		return nil, fmt.Errorf("nil response from upstream")
	}

	if resp.StatusCode >= 400 {
		return &Result{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
	}

	pr, pw := io.Pipe()
	go processCommandCodeStream(resp.Body, pw)

	h := resp.Header.Clone()
	h.Set("Content-Type", "text/event-stream")

	return &Result{StatusCode: resp.StatusCode, Header: h, Body: pr}, nil
}

func randomUUIDSimple() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		_ = err
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
}
