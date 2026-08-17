package executor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func buildKimchiTestPayload() map[string]any {
	return map[string]any{
		"system":            "System rule from top-level",
		"anthropic_version": "2023-06-01",
		"thinking":          map[string]any{"type": "enabled"},
		"reasoning_effort":  "high",
		"messages": []any{
			map[string]any{
				"role":          "user",
				"cache_control": map[string]any{"type": "ephemeral"},
				"content": []any{
					map[string]any{
						"type":          "text",
						"text":          "Hi",
						"cache_control": map[string]any{"type": "ephemeral"},
						"signature":     "sig_123",
					},
				},
			},
			map[string]any{
				"role":              "assistant",
				"content":           "I am thinking",
				"reasoning_content": "This is a long reasoning thought block that should be stripped",
			},
			map[string]any{
				"role":              "assistant",
				"content":           "Short placeholder",
				"reasoning_content": " ", // placeholder <= 8 chars, keep it
			},
		},
		"tools": []any{
			map[string]any{
				"type":          "function",
				"cache_control": map[string]any{"type": "ephemeral"},
				"function":      map[string]any{"name": "test"},
			},
		},
	}
}

func verifyKimchiTopLevelAndSystem(t *testing.T, res map[string]any) {
	t.Helper()

	if _, hasSys := res["system"]; hasSys {
		t.Fatalf("expected top-level system to be deleted")
	}

	if _, hasAV := res["anthropic_version"]; hasAV {
		t.Fatalf("expected anthropic_version to be dropped")
	}

	if _, hasTh := res["thinking"]; hasTh {
		t.Fatalf("expected thinking to be dropped for claude model")
	}

	if _, hasRE := res["reasoning_effort"]; hasRE {
		t.Fatalf("expected reasoning_effort to be dropped for claude model")
	}

	msgs, ok := res["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected messages slice")
	}

	firstMsg, okMsg := msgs[0].(map[string]any)
	if !okMsg || firstMsg["role"] != "system" || firstMsg["content"] != "System rule from top-level" {
		t.Fatalf("expected merged system message, got %v", firstMsg)
	}
}

func verifyKimchiUserMsg(t *testing.T, userMsg map[string]any) {
	t.Helper()

	if _, hasCC := userMsg["cache_control"]; hasCC {
		t.Fatalf("expected msg cache_control dropped")
	}

	parts, okParts := userMsg["content"].([]any)
	if !okParts || len(parts) == 0 {
		t.Fatalf("expected user message content array")
	}

	part0, okP0 := parts[0].(map[string]any)
	if !okP0 {
		t.Fatalf("part0 is not map")
	}

	if _, hasCC := part0["cache_control"]; hasCC {
		t.Fatalf("expected content cache_control dropped")
	}

	if _, hasSig := part0["signature"]; hasSig {
		t.Fatalf("expected signature dropped")
	}
}

func verifyKimchiAsstMsgs(t *testing.T, msgs []any) {
	t.Helper()

	asstMsg, okA := msgs[2].(map[string]any)
	if !okA {
		t.Fatalf("asstMsg is not map")
	}

	if _, hasRC := asstMsg["reasoning_content"]; hasRC {
		t.Fatalf("expected long reasoning_content to be stripped")
	}

	asstMsgShort, okAS := msgs[3].(map[string]any)
	if !okAS {
		t.Fatalf("asstMsgShort is not map")
	}

	rc, okRC := asstMsgShort["reasoning_content"].(string)
	if !okRC || rc != " " {
		t.Fatalf("expected short reasoning_content placeholder preserved, got %q", rc)
	}
}

func verifyKimchiUserAndAsst(t *testing.T, msgs []any) {
	t.Helper()

	userMsg, okU := msgs[1].(map[string]any)
	if !okU {
		t.Fatalf("user message is not map")
	}

	verifyKimchiUserMsg(t, userMsg)
	verifyKimchiAsstMsgs(t, msgs)
}

func verifyKimchiMessagesAndTools(t *testing.T, res map[string]any) {
	t.Helper()

	msgs, ok := res["messages"].([]any)
	if !ok || len(msgs) < 4 {
		t.Fatalf("expected at least 4 messages")
	}

	verifyKimchiUserAndAsst(t, msgs)

	tools, okT := res["tools"].([]any)
	if !okT || len(tools) == 0 {
		t.Fatalf("expected tools slice")
	}

	tool0, okT0 := tools[0].(map[string]any)
	if !okT0 {
		t.Fatalf("tool0 is not map")
	}

	if _, hasCC := tool0["cache_control"]; hasCC {
		t.Fatalf("expected tool cache_control dropped")
	}
}

func TestKimchi_TransformRequest(t *testing.T) {
	body := buildKimchiTestPayload()
	res := transformKimchiRequest("claude-3-5-sonnet", body)

	verifyKimchiTopLevelAndSystem(t, res)
	verifyKimchiMessagesAndTools(t, res)
}

func verifyKimchiMockResponse(t *testing.T, res *Result, receivedBody map[string]any) {
	t.Helper()

	if res == nil || res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %v", res)
	}

	msgs, ok := receivedBody["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected 2 messages received, got %v", receivedBody)
	}
}

func TestKimchi_ExecuteMock(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := json.NewDecoder(r.Body).Decode(&receivedBody)
		if err != nil {
			t.Errorf("decoding request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")

		_, err = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		if err != nil {
			t.Errorf("writing response: %v", err)
		}
	}))
	defer srv.Close()

	ex := NewKimchiExecutor(srv.Client())
	cred := Credentials{
		ProviderSpecificData: nil,
		AccessToken:          "",
		RefreshToken:         "",
		ProjectID:            "",
		APIKey:               "kimchi-key",
		BaseURL:              srv.URL + "/v1",
	}

	body, err := json.Marshal(map[string]any{
		"system": "System instructions",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"client_metadata": map[string]any{"foo": "bar"},
	})
	if err != nil {
		t.Fatalf("json marshal error: %v", err)
	}

	res, err := ex.Execute(context.Background(), cred, "kimchi/qwen", body, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			t.Errorf("closing res body: %v", err)
		}
	}()

	verifyKimchiMockResponse(t, res, receivedBody)
}
