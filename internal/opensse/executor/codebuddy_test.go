package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCodeBuddyCN_TransformRequest(t *testing.T) {
	// Agent system prompt gets neutralized
	body := map[string]any{
		"stream":           false,
		"reasoning_effort": "high",
		"messages": []any{
			map[string]any{
				"role":    "system",
				"content": "You are Claude Code, Anthropic's official CLI for software engineering.",
			},
			map[string]any{
				"role":    "user",
				"content": "Hello",
			},
		},
	}

	res := transformCodeBuddyCN(body)
	if res["stream"] != true {
		t.Fatalf("expected stream forced to true")
	}

	if res["reasoning_summary"] != "auto" {
		t.Fatalf("expected reasoning_summary auto, got %v", res["reasoning_summary"])
	}

	msgs := res["messages"].([]any)

	sysMsg := msgs[0].(map[string]any)
	if sysMsg["content"] != codeBuddyNeutralPrompt {
		t.Fatalf("expected neutral prompt, got %v", sysMsg["content"])
	}

	// Normal user system prompt stays intact
	bodyNormal := map[string]any{
		"messages": []any{
			map[string]any{
				"role":    "system",
				"content": "Be concise.",
			},
		},
	}
	resNormal := transformCodeBuddyCN(bodyNormal)

	msgsNormal := resNormal["messages"].([]any)
	if msgsNormal[0].(map[string]any)["content"] != "Be concise." {
		t.Fatalf("expected normal system prompt preserved")
	}
}

func TestCodeBuddyIntl_TransformRequest(t *testing.T) {
	body := map[string]any{
		"stream":           false,
		"reasoning_effort": "medium",
		"messages": []any{
			map[string]any{
				"role":    "system",
				"content": "Ignored existing system",
			},
			map[string]any{
				"role":    "user",
				"content": "Explain goroutines",
			},
		},
	}

	res := transformCodeBuddyIntl(body)
	if res["stream"] != true {
		t.Fatalf("expected stream forced true")
	}

	if res["reasoning_summary"] != "auto" {
		t.Fatalf("expected reasoning_summary auto")
	}

	msgs := res["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}

	sysMsg := msgs[0].(map[string]any)
	if sysMsg["role"] != "system" || sysMsg["content"] != "You are CodeBuddy Code." {
		t.Fatalf("expected CodeBuddy Code system prompt, got %v", sysMsg)
	}

	userMsg := msgs[1].(map[string]any)
	if userMsg["role"] != "user" {
		t.Fatalf("expected user role")
	}

	blocks, ok := userMsg["content"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected content as typed blocks, got %v", userMsg["content"])
	}

	block0 := blocks[0].(map[string]any)
	if block0["type"] != "text" || block0["text"] != "Explain goroutines" {
		t.Fatalf("expected typed text block, got %v", block0)
	}
}

func TestCodeBuddy_ExecuteMock(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	ex := NewCodeBuddyExecutor("codebuddy-cn", srv.Client())
	cred := Credentials{
		APIKey:  "cb-key",
		BaseURL: srv.URL + "/v2",
	}

	body, _ := json.Marshal(map[string]any{
		"stream": false,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})

	res, err := ex.Execute(context.Background(), cred, "copilot", body, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	if receivedBody["stream"] != true {
		t.Fatalf("expected stream forced to true")
	}

	_, _ = io.ReadAll(res.Body)
}
