package executor_test

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTraeExecutor_ExecutionAndStreaming(t *testing.T) {
	var gotAuth string

	var gotCreateSessionBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")

		if r.URL.Path == "/chat_sessions" {
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&gotCreateSessionBody)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"chat_session_id": "session-trae-123",
					"message_id":      "msg-trae-456",
				},
			})

			return
		}

		if strings.HasPrefix(r.URL.Path, "/chat_sessions/session-trae-123/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`event: plan_item
data: {"id":"item1","thought":"Hello "}

event: plan_item
data: {"id":"item1","thought":"Hello from Trae SOLO"}

event: token_usage
data: {"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}

event: done
data: {}

`))

			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	ex := executor.GetExecutor("trae")
	if ex == nil {
		t.Fatal("trae executor not registered")
	}

	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "build an app"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken: "jwt-token-999",
		BaseURL:     srv.URL,
	}, "gemini-3.1-pro", body, true)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	if gotAuth != "Cloud-IDE-JWT jwt-token-999" {
		t.Errorf("got auth %q", gotAuth)
	}

	outBytes, _ := io.ReadAll(res.Body)

	outStr := string(outBytes)
	if !strings.Contains(outStr, "Hello ") || !strings.Contains(outStr, "from Trae SOLO") {
		t.Errorf("stream missing content: %s", outStr)
	}

	if !strings.Contains(outStr, "[DONE]") {
		t.Errorf("stream missing [DONE]: %s", outStr)
	}
}

func TestTraeExecutor_NonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat_sessions" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"chat_session_id": "session-1",
					"message_id":      "msg-1",
				},
			})

			return
		}

		if strings.HasPrefix(r.URL.Path, "/chat_sessions/session-1/events") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`event: plan_item
data: {"id":"item1","thought":"Unary Trae result"}

event: done
data: {}
`))

			return
		}
	}))
	defer srv.Close()

	ex := executor.GetExecutor("trae")
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "ping"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken: "jwt-token-1",
		BaseURL:     srv.URL,
	}, "work", body, false)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	var respObj map[string]any
	if err := json.NewDecoder(res.Body).Decode(&respObj); err != nil {
		t.Fatal(err)
	}

	choices, _ := respObj["choices"].([]any)
	if len(choices) == 0 {
		t.Fatal("empty choices")
	}

	first, _ := choices[0].(map[string]any)

	msg, _ := first["message"].(map[string]any)
	if msg["content"] != "Unary Trae result" {
		t.Errorf("content = %v, want Unary Trae result", msg["content"])
	}
}
