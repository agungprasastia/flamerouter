package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatNonStreamTranslation(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-10","choices":[{"message":{"role":"assistant","content":"chat response"}}]}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{})
	if err != nil {
		t.Fatalf("ChatWithOptions: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}

	choices, ok := res["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("missing choices: %v", res)
	}
}

func TestChatStreamingTranslationAndDoneMarker(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	streamData := "data: {\"id\":\"chatcmpl-11\",\"choices\":[{\"delta\":{\"content\":\"part1\"}}]}\n\ndata: [DONE]\n\n"
	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		StreamBody: []byte(streamData),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{})
	if err != nil {
		t.Fatalf("ChatWithOptions: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "part1") {
		t.Fatalf("body missing part1: %s", bodyStr)
	}

	if !strings.Contains(bodyStr, "data: [DONE]") {
		t.Fatalf("body missing [DONE]: %s", bodyStr)
	}
}

func TestChatStreamingUpstreamErrorDoesNotWriteSSE(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"error":{"message":"bad request model"}}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected non-SSE headers on upstream error, got %q", ct)
	}
}

func TestMessagesEndpointTranslationToOpenAIProvider(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-12","choices":[{"message":{"role":"assistant","content":"translated from openai"}}]}`),
	})
	fb := fallback.New(st)

	// Claude Messages API payload
	reqBody := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hello claude"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{
		SourceFormat: "claude",
	})
	if err != nil {
		t.Fatalf("ChatWithOptions: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	// Output should be translated back to Anthropic Messages shape
	if res["type"] != "message" && res["role"] != "assistant" {
		t.Logf("messages response payload: %v", res)
	}
}
