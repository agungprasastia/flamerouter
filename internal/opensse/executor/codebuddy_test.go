package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func verifyCodeBuddyCNNeutralSystem(t *testing.T, res map[string]any) {
	t.Helper()

	msgs, ok := res["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected messages slice")
	}

	sysMsg, okSys := msgs[0].(map[string]any)
	if !okSys || sysMsg["content"] != codeBuddyNeutralPrompt {
		t.Fatalf("expected neutral prompt, got %v", sysMsg["content"])
	}
}

func verifyCodeBuddyCNNormalSystem(t *testing.T, resNormal map[string]any) {
	t.Helper()

	msgsNormal, okNorm := resNormal["messages"].([]any)
	if !okNorm || len(msgsNormal) == 0 {
		t.Fatalf("expected normal messages slice")
	}

	msg0, okMsg0 := msgsNormal[0].(map[string]any)
	if !okMsg0 || msg0["content"] != "Be concise." {
		t.Fatalf("expected normal system prompt preserved")
	}
}

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

	verifyCodeBuddyCNNeutralSystem(t, res)

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
	verifyCodeBuddyCNNormalSystem(t, resNormal)
}

func verifyCodeBuddyIntlMessages(t *testing.T, res map[string]any) {
	t.Helper()

	msgs, ok := res["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %v", res["messages"])
	}

	sysMsg, okSys := msgs[0].(map[string]any)
	if !okSys || sysMsg["role"] != "system" || sysMsg["content"] != "You are CodeBuddy Code." {
		t.Fatalf("expected CodeBuddy Code system prompt, got %v", sysMsg)
	}

	verifyCodeBuddyUserMessage(t, msgs[1])
}

func verifyCodeBuddyUserMessage(t *testing.T, msg any) {
	t.Helper()

	userMsg, okUser := msg.(map[string]any)
	if !okUser || userMsg["role"] != "user" {
		t.Fatalf("expected user role")
	}

	blocks, ok := userMsg["content"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("expected content as typed blocks, got %v", userMsg["content"])
	}

	block0, okB0 := blocks[0].(map[string]any)
	if !okB0 || block0["type"] != "text" || block0["text"] != "Explain goroutines" {
		t.Fatalf("expected typed text block, got %v", block0)
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

	verifyCodeBuddyIntlMessages(t, res)
}

func TestCodeBuddy_ExecuteMock(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := json.NewDecoder(r.Body).Decode(&receivedBody)
		if err != nil {
			t.Errorf("decoding request: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")

		_, err = w.Write([]byte("data: [DONE]\n\n"))
		if err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer srv.Close()

	ex := NewCodeBuddyExecutor("codebuddy-cn", srv.Client())
	cred := Credentials{
		ProviderSpecificData: nil,
		AccessToken:          "",
		RefreshToken:         "",
		ProjectID:            "",
		APIKey:               "cb-key",
		BaseURL:              srv.URL + "/v2",
	}

	body, err := json.Marshal(map[string]any{
		"stream": false,
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}

	res, err := ex.Execute(context.Background(), cred, "copilot", body, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() {
		if cErr := res.Body.Close(); cErr != nil {
			t.Errorf("closing response body: %v", cErr)
		}
	}()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	if receivedBody["stream"] != true {
		t.Fatalf("expected stream forced to true")
	}

	_, err = io.ReadAll(res.Body)
	if err != nil {
		t.Errorf("reading response: %v", err)
	}
}
