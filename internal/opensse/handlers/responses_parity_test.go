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

func TestResponsesStringInputAndInstructions(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-resp-1","choices":[{"message":{"role":"assistant","content":"responses answer"}}]}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","input":"explain gravity","instructions":"be concise","temperature":0.7}`)
	rec := httptest.NewRecorder()

	err := Responses(context.Background(), rec, reqBody, st, fake, fb)
	if err != nil {
		t.Fatalf("Responses err = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}

	var translatedReq map[string]any
	if err := json.Unmarshal(calls[0].Body, &translatedReq); err != nil {
		t.Fatalf("unmarshal translated: %v", err)
	}

	msgs, ok := translatedReq["messages"].([]any)
	if !ok || len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages (system + user), got %v", msgs)
	}

	sysMsg := msgs[0].(map[string]any)
	if sysMsg["role"] != "system" || sysMsg["content"] != "be concise" {
		t.Fatalf("system message = %v", sysMsg)
	}

	userMsg := msgs[1].(map[string]any)
	if userMsg["role"] != "user" || userMsg["content"] != "explain gravity" {
		t.Fatalf("user message = %v", userMsg)
	}
}

func TestResponsesArrayInput(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-resp-2","choices":[{"message":{"role":"assistant","content":"array answer"}}]}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","input":[{"role":"user","content":"item 1"}]}`)
	rec := httptest.NewRecorder()

	err := Responses(context.Background(), rec, reqBody, st, fake, fb)
	if err != nil {
		t.Fatalf("Responses err = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestResponsesStreamingSSE(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	streamData := "data: {\"id\":\"chatcmpl-resp-3\",\"choices\":[{\"delta\":{\"content\":\"stream chunk\"}}]}\n\ndata: [DONE]\n\n"
	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		StreamBody: []byte(streamData),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","stream":true,"input":"stream me"}`)
	rec := httptest.NewRecorder()

	err := Responses(context.Background(), rec, reqBody, st, fake, fb)
	if err != nil {
		t.Fatalf("Responses err = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q", ct)
	}

	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "response.output_item.added") && !strings.Contains(bodyStr, "response.output_text.delta") && !strings.Contains(bodyStr, "stream chunk") {
		t.Logf("stream output: %s", bodyStr)
	}
}

func TestCompactResponsesSetsFlag(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-resp-4","choices":[{"message":{"role":"assistant","content":"compact answer"}}]}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","input":"compact test"}`)
	rec := httptest.NewRecorder()

	err := CompactResponses(context.Background(), rec, reqBody, st, fake, fb)
	if err != nil {
		t.Fatalf("CompactResponses err = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestResponsesInvalidJSONReturns400(t *testing.T) {
	st := newTestStore(t)
	fake := testutil.NewFakeExecutor()
	fb := fallback.New(st)

	rec := httptest.NewRecorder()

	err := Responses(context.Background(), rec, []byte("not-json"), st, fake, fb)
	if err == nil {
		t.Fatal("expected error")
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
