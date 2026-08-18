package handlers

import (
	"bytes"
	"context"
	"flamerouter/internal/opensse/executor"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/rtk"
	"flamerouter/internal/opensse/testutil"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testUsageSink struct {
	provider   string
	model      string
	prompt     int
	completion int
	cached     int
	statusCode int
	called     bool
}

func (s *testUsageSink) OnUsage(provider, model, _ string, prompt, completion, cached, statusCode int) {
	s.called = true
	s.provider = provider
	s.model = model
	s.prompt = prompt
	s.completion = completion
	s.cached = cached
	s.statusCode = statusCode
}

func TestUsageSinkRecordedOnSuccess(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: []byte(`{
			"id":"chatcmpl-usage-1",
			"choices":[{"message":{"role":"assistant","content":"usage content"}}],
			"usage":{"prompt_tokens":15,"completion_tokens":25}
		}`),
		StreamBody: nil,
	})
	fb := fallback.New(st)
	sink := &testUsageSink{
		provider:   "",
		model:      "",
		prompt:     0,
		completion: 0,
		cached:     0,
		statusCode: 0,
		called:     false,
	}

	reqBody := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"test usage"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{
		Usage:           sink,
		ClientHeaders:   nil,
		SourceFormat:    "",
		AccountStrategy: "",
		TokenSaver:      rtk.EmptyTokenSaver(),
		StickyLimit:     0,
	})
	if err != nil {
		t.Fatalf("ChatWithOptions: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if !sink.called {
		t.Fatal("expected UsageSink.OnUsage to be called")
	}

	if sink.provider != "openai" || sink.model != "gpt-4o" {
		t.Fatalf("sink provider/model = %s/%s", sink.provider, sink.model)
	}

	if sink.prompt != 15 || sink.completion != 25 {
		t.Fatalf("sink prompt/completion = %d/%d", sink.prompt, sink.completion)
	}

	if sink.statusCode != http.StatusOK {
		t.Fatalf("sink status code = %d", sink.statusCode)
	}
}

func TestTokenSaverHeaderOptOut(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-ts-1","choices":[{"message":{"role":"assistant","content":"ok"}}]}`),
		StreamBody: nil,
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	rec := httptest.NewRecorder()

	headers := http.Header{}
	headers.Set("x-9router-token-saver", "off")

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{
		Usage:           nil,
		ClientHeaders:   headers,
		SourceFormat:    "",
		AccountStrategy: "",
		TokenSaver:      rtk.EmptyTokenSaver(),
		StickyLimit:     0,
	})
	if err != nil {
		t.Fatalf("ChatWithOptions: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
}

func TestUsageSinkRecordedOnStreamSuccess(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       nil,
		StreamBody: []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{}}],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":8}}\n\ndata: [DONE]\n\n"),
	})
	fb := fallback.New(st)
	sink := &testUsageSink{
		provider:   "",
		model:      "",
		prompt:     0,
		completion: 0,
		cached:     0,
		statusCode: 0,
		called:     false,
	}

	reqBody := []byte(`{"model":"openai/gpt-4o","stream":true,"messages":[{"role":"user","content":"test stream usage"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{
		Usage:           sink,
		ClientHeaders:   nil,
		SourceFormat:    "",
		AccountStrategy: "",
		TokenSaver:      rtk.EmptyTokenSaver(),
		StickyLimit:     0,
	})
	if err != nil {
		t.Fatalf("ChatWithOptions: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if !sink.called {
		t.Fatal("expected UsageSink.OnUsage to be called for streaming response")
	}

	if sink.provider != "openai" || sink.model != "gpt-4o" {
		t.Fatalf("sink provider/model = %s/%s", sink.provider, sink.model)
	}

	if sink.prompt != 12 || sink.completion != 8 {
		t.Fatalf("sink prompt/completion = %d/%d", sink.prompt, sink.completion)
	}

	if sink.statusCode != http.StatusOK {
		t.Fatalf("sink status code = %d", sink.statusCode)
	}
}

func TestWriteResultRecordUsageFromEmbeddingsResponse(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	sink := &testUsageSink{
		provider:   "",
		model:      "",
		prompt:     0,
		completion: 0,
		cached:     0,
		statusCode: 0,
		called:     false,
	}
	rec := httptest.NewRecorder()

	res := &executor.Result{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewReader([]byte(`{
			"object":"list",
			"data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],
			"model":"text-embedding-3-small",
			"usage":{"prompt_tokens":100,"total_tokens":100}
		}`))),
	}

	err := writeResultRecordUsage(rec, res, st, "openai", "text-embedding-3-small", "conn1", []byte(`{"model":"text-embedding-3-small","input":"hello"}`), sink)
	if err != nil {
		t.Fatalf("writeResultRecordUsage: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	if !sink.called {
		t.Fatal("expected UsageSink.OnUsage to be called")
	}

	if sink.provider != "openai" || sink.model != "text-embedding-3-small" {
		t.Fatalf("sink provider/model = %s/%s", sink.provider, sink.model)
	}

	if sink.prompt != 100 || sink.completion != 0 || sink.cached != 0 {
		t.Fatalf("sink prompt/completion/cached = %d/%d/%d, want 100/0/0", sink.prompt, sink.completion, sink.cached)
	}

	if sink.statusCode != http.StatusOK {
		t.Fatalf("sink status code = %d", sink.statusCode)
	}
}

func TestUsageSinkRecordedOnNonStreamWithoutUsageObject(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"choices":[{"message":{"role":"assistant","content":"Hello response"}}]}`),
		StreamBody: nil,
	})
	fb := fallback.New(st)
	sink := &testUsageSink{
		provider:   "",
		model:      "",
		prompt:     0,
		completion: 0,
		cached:     0,
		statusCode: 0,
		called:     false,
	}

	reqBody := []byte(`{"model":"openai/gpt-4o","stream":false,"messages":[{"role":"user","content":"hi test"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{
		Usage:           sink,
		ClientHeaders:   nil,
		SourceFormat:    "",
		AccountStrategy: "",
		TokenSaver:      rtk.EmptyTokenSaver(),
		StickyLimit:     0,
	})
	if err != nil {
		t.Fatalf("ChatWithOptions: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if !sink.called {
		t.Fatal("expected UsageSink.OnUsage to be called for non-stream response")
	}

	if sink.provider != "openai" || sink.model != "gpt-4o" {
		t.Fatalf("sink provider/model = %s/%s", sink.provider, sink.model)
	}

	if sink.prompt <= 0 || sink.completion <= 0 {
		t.Fatalf("expected estimated tokens > 0, got prompt=%d, completion=%d", sink.prompt, sink.completion)
	}
}
