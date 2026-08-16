package handlers

import (
	"context"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/testutil"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testUsageSink struct {
	provider   string
	model      string
	prompt     int
	completion int
	statusCode int
	called     bool
}

func (s *testUsageSink) OnUsage(provider, model, connectionID string, prompt, completion, statusCode int) {
	s.called = true
	s.provider = provider
	s.model = model
	s.prompt = prompt
	s.completion = completion
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
	})
	fb := fallback.New(st)
	sink := &testUsageSink{}

	reqBody := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"test usage"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{
		Usage: sink,
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
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	rec := httptest.NewRecorder()

	headers := http.Header{}
	headers.Set("x-9router-token-saver", "off")

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{
		ClientHeaders: headers,
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
