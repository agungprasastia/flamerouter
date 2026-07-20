package executor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
)

func init() {
	RegisterSpecialized("commandcode", &CommandCodeExecutor{
		Base: Base{
			Provider: "commandcode",
			BaseURL:  "https://api.commandcode.ai/alpha/generate",
		},
	})
}

type CommandCodeExecutor struct {
	Base
}

func (e *CommandCodeExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	m["stream"] = true // always stream NDJSON upstream
	m["model"] = model
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	url := e.BaseURL
	if base := strings.TrimRight(cred.BaseURL, "/"); base != "" {
		url = base
		if !strings.Contains(base, "/alpha/") {
			url = base + "/alpha/generate"
		}
	}

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

	resp, err := e.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return &Result{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), Body: resp.Body}, nil
	}

	// Wrap NDJSON → OpenAI SSE
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		defer resp.Body.Close()
		state := concerns.NewResponseState()
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var event map[string]any
			if json.Unmarshal([]byte(line), &event) != nil {
				// translator expects map; pass via wrapper
				event = map[string]any{"_raw": line}
			}
			// Prefer raw line path: commandcode translator accepts map with type fields
			// Re-parse as generic: put line as stream event fields
			var raw map[string]any
			if json.Unmarshal([]byte(line), &raw) == nil {
				chunks := translator.DefaultRegistry.TranslateResponse(
					translator.FormatCommandCode, translator.FormatOpenAI, raw, state,
				)
				for _, c := range chunks {
					j, _ := json.Marshal(c)
					pw.Write([]byte("data: " + string(j) + "\n\n"))
				}
			}
		}
		pw.Write([]byte("data: [DONE]\n\n"))
	}()

	h := resp.Header.Clone()
	h.Set("Content-Type", "text/event-stream")
	return &Result{StatusCode: resp.StatusCode, Header: h, Body: pr}, nil
}

func randomUUIDSimple() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
}
