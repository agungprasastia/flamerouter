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

func mockTraeServer(gotAuth *string, gotBody *map[string]any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAuth = r.Header.Get("Authorization")

		if r.URL.Path == "/chat_sessions" {
			defer func() {
				_ = r.Body.Close() // nolint:errcheck
			}()

			_ = json.NewDecoder(r.Body).Decode(gotBody) // nolint:errcheck

			w.Header().Set("Content-Type", "application/json")

			_ = json.NewEncoder(w).Encode(map[string]any{ // nolint:errcheck
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
			_, _ = w.Write([]byte("event: plan_item\ndata: {\"id\":\"item1\",\"thought\":\"Hello \"}\n\nevent: plan_item\ndata: {\"id\":\"item1\",\"thought\":\"Hello from Trae SOLO\"}\n\nevent: token_usage\ndata: {\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}\n\nevent: done\ndata: {}\n\n")) // nolint:errcheck

			return
		}

		http.NotFound(w, r)
	}))
}

func TestTraeExecutor_ExecutionAndStreaming(t *testing.T) {
	var (
		gotAuth              string
		gotCreateSessionBody map[string]any
	)

	srv := mockTraeServer(&gotAuth, &gotCreateSessionBody)
	defer srv.Close()

	ex := executor.GetExecutor("trae")
	if ex == nil {
		t.Fatal("trae executor not registered")
	}

	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]string{{"role": "user", "content": "build an app"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:               "",
		AccessToken:          "jwt-token-999",
		RefreshToken:         "",
		BaseURL:              srv.URL,
		ProviderSpecificData: nil,
		ProjectID:            "",
	}, "gemini-3.1-pro", body, true)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }() // nolint:errcheck

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	if gotAuth != "Cloud-IDE-JWT jwt-token-999" {
		t.Errorf("got auth %q", gotAuth)
	}

	outBytes, _ := io.ReadAll(res.Body) // nolint:errcheck

	outStr := string(outBytes)
	if !strings.Contains(outStr, "Hello ") || !strings.Contains(outStr, "from Trae SOLO") {
		t.Errorf("stream missing content: %s", outStr)
	}

	if !strings.Contains(outStr, "[DONE]") {
		t.Errorf("stream missing [DONE]: %s", outStr)
	}
}

func mockTraeNonStreamServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat_sessions" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{ // nolint:errcheck
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
			_, _ = w.Write([]byte("event: plan_item\ndata: {\"id\":\"item1\",\"thought\":\"Unary Trae result\"}\n\nevent: done\ndata: {}\n")) // nolint:errcheck

			return
		}
	}))
}

func TestTraeExecutor_NonStreaming(t *testing.T) {
	srv := mockTraeNonStreamServer()
	defer srv.Close()

	ex := executor.GetExecutor("trae")
	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]string{{"role": "user", "content": "ping"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:               "",
		AccessToken:          "jwt-token-1",
		RefreshToken:         "",
		BaseURL:              srv.URL,
		ProviderSpecificData: nil,
		ProjectID:            "",
	}, "work", body, false)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = res.Body.Close() }() // nolint:errcheck

	var respObj map[string]any
	if err := json.NewDecoder(res.Body).Decode(&respObj); err != nil {
		t.Fatal(err)
	}

	choices, ok := respObj["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatal("empty choices")
	}

	first, ok := choices[0].(map[string]any)
	if !ok {
		t.Fatal("first choice not map")
	}

	msg, ok := first["message"].(map[string]any)
	if !ok || msg["content"] != "Unary Trae result" {
		t.Errorf("content = %v, want Unary Trae result", msg["content"])
	}
}
