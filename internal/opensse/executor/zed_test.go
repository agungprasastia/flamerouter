package executor_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"flamerouter/internal/opensse/executor"
)

func TestZedExecutor_ExecutionAndStreaming(t *testing.T) {
	var gotLlmTokenReq bool
	var gotCompletionReq bool
	var gotAuthHeader string
	var gotPayload map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/llm_tokens" {
			gotLlmTokenReq = true
			if r.Header.Get("Authorization") != "user-123 token-abc" {
				t.Errorf("unexpected user auth header: %s", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "zed-llm-token-xyz",
			})
			return
		}

		if r.URL.Path == "/completions" {
			gotCompletionReq = true
			gotAuthHeader = r.Header.Get("Authorization")
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&gotPayload)

			w.Header().Set("Content-Type", "application/x-ndjson")
			// send NDJSON with event
			_, _ = w.Write([]byte(`{"event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello Zed"}}}
{"status":{"type":"stream_ended"}}
`))
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	ex := executor.GetExecutor("zed")
	if ex == nil {
		t.Fatal("zed executor not registered")
	}

	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		AccessToken: "token-abc",
		BaseURL:     srv.URL,
		ProviderSpecificData: map[string]any{
			"userId":         "user-123",
			"organizationId": "org-456",
		},
	}, "claude-3-5-sonnet", body, true)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if !gotLlmTokenReq {
		t.Error("did not request llm token")
	}
	if !gotCompletionReq {
		t.Error("did not request completions")
	}
	if gotAuthHeader != "Bearer zed-llm-token-xyz" {
		t.Errorf("got auth %q, want Bearer zed-llm-token-xyz", gotAuthHeader)
	}
	if gotPayload["provider"] != "Anthropic" {
		t.Errorf("provider = %v, want Anthropic", gotPayload["provider"])
	}

	outBytes, _ := io.ReadAll(res.Body)
	outStr := string(outBytes)
	if !strings.Contains(outStr, "Hello Zed") {
		t.Errorf("response stream missing text: %s", outStr)
	}
	if !strings.Contains(outStr, "[DONE]") {
		t.Errorf("response stream missing [DONE]: %s", outStr)
	}
}
