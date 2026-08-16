package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKimchi_TransformRequest(t *testing.T) {
	body := map[string]any{
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

	res := transformKimchiRequest("claude-3-5-sonnet", body)

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

	msgs := res["messages"].([]any)
	// Leading system message merged
	firstMsg := msgs[0].(map[string]any)
	if firstMsg["role"] != "system" || firstMsg["content"] != "System rule from top-level" {
		t.Fatalf("expected merged system message, got %v", firstMsg)
	}

	userMsg := msgs[1].(map[string]any)
	if _, hasCC := userMsg["cache_control"]; hasCC {
		t.Fatalf("expected msg cache_control dropped")
	}

	parts := userMsg["content"].([]any)

	part0 := parts[0].(map[string]any)
	if _, hasCC := part0["cache_control"]; hasCC {
		t.Fatalf("expected content cache_control dropped")
	}

	if _, hasSig := part0["signature"]; hasSig {
		t.Fatalf("expected signature dropped")
	}

	asstMsg := msgs[2].(map[string]any)
	if _, hasRC := asstMsg["reasoning_content"]; hasRC {
		t.Fatalf("expected long reasoning_content to be stripped")
	}

	asstMsgShort := msgs[3].(map[string]any)
	if rc, _ := asstMsgShort["reasoning_content"].(string); rc != " " {
		t.Fatalf("expected short reasoning_content placeholder preserved, got %q", rc)
	}

	tools := res["tools"].([]any)

	tool0 := tools[0].(map[string]any)
	if _, hasCC := tool0["cache_control"]; hasCC {
		t.Fatalf("expected tool cache_control dropped")
	}
}

func TestKimchi_ExecuteMock(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	ex := NewKimchiExecutor(srv.Client())
	cred := Credentials{
		APIKey:  "kimchi-key",
		BaseURL: srv.URL + "/v1",
	}
	body, _ := json.Marshal(map[string]any{
		"system": "System instructions",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"client_metadata": map[string]any{"foo": "bar"},
	})

	res, err := ex.Execute(context.Background(), cred, "kimchi/qwen", body, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	if _, hasCM := receivedBody["client_metadata"]; hasCM {
		t.Fatalf("expected client_metadata dropped")
	}

	msgs, ok := receivedBody["messages"].([]any)
	if !ok || len(msgs) < 2 {
		t.Fatalf("expected system message prepended to messages, got %v", receivedBody["messages"])
	}

	_, _ = io.ReadAll(res.Body)
}
