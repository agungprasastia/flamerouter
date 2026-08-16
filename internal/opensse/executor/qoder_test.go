package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQoder_NormalizeMessages(t *testing.T) {
	raw := []any{
		map[string]any{"role": "system", "content": "You are system"},
		map[string]any{"role": "user", "content": "hello"},
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"text": "part 1"},
				map[string]any{"text": "part 2"},
			},
		},
	}
	msgs, sys := normalizeQoderMessages(raw)
	if sys != "You are system" {
		t.Fatalf("expected 'You are system', got %q", sys)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0]["content"] != "hello" {
		t.Fatalf("expected 'hello', got %v", msgs[0]["content"])
	}
	if msgs[1]["content"] != "part 1\npart 2" {
		t.Fatalf("expected 'part 1\npart 2', got %v", msgs[1]["content"])
	}
}

func TestQoder_BuildRequestBody(t *testing.T) {
	cred := Credentials{
		AccessToken: "acc-token-123",
		ProviderSpecificData: map[string]any{
			"userId": "user-456",
		},
	}
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "System instructions"},
			map[string]any{"role": "user", "content": "Write a go function"},
		},
		"max_tokens": float64(1024),
	}
	qoderKey, payload, err := buildQoderRequestBody("qoder/qmodel", body, cred)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if qoderKey != "qmodel" {
		t.Fatalf("expected qmodel, got %s", qoderKey)
	}
	if payload["system"] != "System instructions" {
		t.Fatalf("expected system text hoisted, got %v", payload["system"])
	}
	msgs := payload["messages"].([]map[string]any)
	if len(msgs) != 1 || msgs[0]["role"] != "user" {
		t.Fatalf("expected 1 user message, got %v", msgs)
	}
	params := payload["parameters"].(map[string]any)
	if params["max_tokens"] != 1024 {
		t.Fatalf("expected max_tokens 1024, got %v", params["max_tokens"])
	}
}

func TestQoder_ExecuteMissingCredentials(t *testing.T) {
	ex := NewQoderExecutor(nil)
	res, err := ex.Execute(context.Background(), Credentials{}, "qoder/auto", []byte(`{}`), true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", res.StatusCode)
	}
}

func TestQoder_ExecuteSuccessAndWrapSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Model-Key") != "auto" {
			t.Errorf("expected X-Model-Key auto, got %s", r.Header.Get("X-Model-Key"))
		}
		if r.Header.Get("Cosy-User") != "u123" {
			t.Errorf("expected Cosy-User u123, got %s", r.Header.Get("Cosy-User"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"statusCodeValue\":200,\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"Hello\\\"}}]}\"}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	ex := NewQoderExecutor(srv.Client())
	ex.BaseURL = srv.URL
	cred := Credentials{
		AccessToken: "token-abc",
		ProviderSpecificData: map[string]any{
			"userId": "u123",
		},
	}
	body, _ := json.Marshal(map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "Hi"},
		},
	})
	res, err := ex.Execute(context.Background(), cred, "qoder/auto", body, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	outBytes, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	outStr := string(outBytes)
	if !strings.Contains(outStr, `data: {"choices":[{"delta":{"content":"Hello"}}]}`) {
		t.Fatalf("expected unwrapped data, got %q", outStr)
	}
	if !strings.Contains(outStr, "data: [DONE]") {
		t.Fatalf("expected [DONE], got %q", outStr)
	}
}
