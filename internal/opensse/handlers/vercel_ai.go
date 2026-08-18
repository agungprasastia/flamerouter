package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/rtk"
	"flamerouter/internal/store"
	"net/http"
	"strings"
	"time"
)

// VercelAIChat handles POST /v1/api/chat — Vercel AI / Ollama-shaped response.
// Parity: run Chat then transform non-stream JSON to Ollama format.
func VercelAIChat(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback) error {
	return VercelAIChatWithOptions(ctx, w, body, st, exec, fb, nil)
}

// VercelAIChatWithOptions handles POST /v1/api/chat with an optional UsageSink.
func VercelAIChatWithOptions(ctx context.Context, w http.ResponseWriter, body []byte, st *store.Store, exec executor.Executor, fb *fallback.Fallback, usageSink UsageSink) error {
	modelName := "llama3.2"

	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		if s, ok := m["model"].(string); ok && s != "" {
			modelName = s
		}
	}

	// Capture chat response then transform if JSON completion
	cw := &captureWriter{
		header: make(http.Header),
		code:   200,
		buf:    bytes.Buffer{},
	}
	err := ChatWithOptions(ctx, cw, body, st, exec, fb, ChatOptions{
		Usage:           usageSink,
		ClientHeaders:   nil,
		SourceFormat:    "",
		AccountStrategy: "",
		TokenSaver:      rtk.EmptyTokenSaver(),
		StickyLimit:     0,
	})

	if cw.code >= 400 || err != nil {
		copyHeader(w.Header(), cw.header)
		w.WriteHeader(cw.code)

		if cw.buf.Len() > 0 {
			_, _ = w.Write(cw.buf.Bytes()) //nolint:errcheck // best effort write
		}

		return err
	}

	ct := cw.header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") || bytes.HasPrefix(bytes.TrimSpace(cw.buf.Bytes()), []byte("data:")) {
		// stream as-is
		copyHeader(w.Header(), cw.header)
		w.WriteHeader(cw.code)
		_, _ = w.Write(cw.buf.Bytes()) //nolint:errcheck // best effort write

		return nil
	}

	var openai map[string]any
	if err := json.Unmarshal(cw.buf.Bytes(), &openai); err != nil {
		copyHeader(w.Header(), cw.header)
		w.WriteHeader(cw.code)
		_, _ = w.Write(cw.buf.Bytes()) //nolint:errcheck // best effort write

		return err
	}

	ollama := transformToOllama(openai, modelName)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	return json.NewEncoder(w).Encode(ollama)
}

func transformToOllama(openai map[string]any, modelName string) map[string]any {
	content := extractChoiceContent(openai)

	out := map[string]any{
		"model":      modelName,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"message": map[string]any{
			"role":    "assistant",
			"content": content,
		},
		"done": true,
	}
	if u, ok := openai["usage"].(map[string]any); ok {
		out["prompt_eval_count"] = u["prompt_tokens"]
		out["eval_count"] = u["completion_tokens"]
	}

	return out
}

func extractChoiceContent(openai map[string]any) string {
	choices, ok := openai["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}

	c0, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}

	if msg, ok := c0["message"].(map[string]any); ok {
		if s, ok := msg["content"].(string); ok {
			return s
		}
	}

	if s, ok := c0["text"].(string); ok {
		return s
	}

	return ""
}

type captureWriter struct {
	header http.Header
	buf    bytes.Buffer
	code   int
}

func (c *captureWriter) Header() http.Header         { return c.header }
func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }
func (c *captureWriter) WriteHeader(statusCode int)  { c.code = statusCode }

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
