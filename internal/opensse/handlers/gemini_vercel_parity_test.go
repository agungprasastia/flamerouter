package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/testutil"
)

func TestGeminiV1BetaListModels(t *testing.T) {
	st := newTestStore(t)
	fake := testutil.NewFakeExecutor()
	fb := fallback.New(st)

	req := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	rec := httptest.NewRecorder()

	err := GeminiV1Beta(context.Background(), rec, req, st, fake, fb)
	if err != nil {
		t.Fatalf("GeminiV1Beta: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	models, ok := res["models"].([]any)
	if !ok || len(models) == 0 {
		t.Fatalf("missing models array: %v", res)
	}
}

func TestGeminiV1BetaGenerateContentConvert(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-gemini-1","choices":[{"message":{"role":"assistant","content":"gemini answer"}}]}`),
	})
	fb := fallback.New(st)

	geminiReq := []byte(`{
		"contents": [{"role":"user","parts":[{"text":"hello from gemini"}]}],
		"generationConfig": {"temperature":0.2,"maxOutputTokens":100}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1beta/models/openai/gpt-4o:generateContent", bytes.NewReader(geminiReq))
	rec := httptest.NewRecorder()

	err := GeminiV1Beta(context.Background(), rec, req, st, fake, fb)
	if err != nil {
		t.Fatalf("GeminiV1Beta err: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Model != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", calls[0].Model)
	}
}

func TestVercelAIChatNonStreamTransformsToOllama(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: []byte(`{
			"id":"chatcmpl-vercel-1",
			"choices":[{"message":{"role":"assistant","content":"vercel result"}}],
			"usage":{"prompt_tokens":12,"completion_tokens":8}
		}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"test vercel"}]}`)
	rec := httptest.NewRecorder()

	err := VercelAIChat(context.Background(), rec, reqBody, st, fake, fb)
	if err != nil {
		t.Fatalf("VercelAIChat: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if res["done"] != true {
		t.Fatalf("done != true: %v", res)
	}
	msg, ok := res["message"].(map[string]any)
	if !ok || msg["content"] != "vercel result" {
		t.Fatalf("unexpected message: %v", res)
	}
	if res["prompt_eval_count"] != float64(12) || res["eval_count"] != float64(8) {
		t.Fatalf("usage not mapped: %v", res)
	}
}

func TestVercelAIChatStreamingPassesThrough(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	streamData := "data: {\"choices\":[{\"delta\":{\"content\":\"chunk\"}}]}\n\ndata: [DONE]\n\n"
	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		StreamBody: []byte(streamData),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","stream":true,"messages":[{"role":"user","content":"stream vercel"}]}`)
	rec := httptest.NewRecorder()

	err := VercelAIChat(context.Background(), rec, reqBody, st, fake, fb)
	if err != nil {
		t.Fatalf("VercelAIChat stream: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data:") {
		t.Fatalf("expected stream data: %s", rec.Body.String())
	}
}
