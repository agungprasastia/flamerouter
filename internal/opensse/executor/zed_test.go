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

func handleMockZedLlmToken(t *testing.T, w http.ResponseWriter, r *http.Request, gotLlmTokenReq *bool) {
	*gotLlmTokenReq = true

	if r.Header.Get("Authorization") != "user-123 token-abc" {
		t.Errorf("unexpected user auth header: %s", r.Header.Get("Authorization"))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{ // nolint:errcheck
		"token": "zed-llm-token-xyz",
	})
}

func handleMockZedCompletions(w http.ResponseWriter, r *http.Request, gotCompletionReq *bool, gotAuthHeader *string, gotPayload *map[string]any) {
	*gotCompletionReq = true
	*gotAuthHeader = r.Header.Get("Authorization")

	defer func() { _ = r.Body.Close() }() // nolint:errcheck

	_ = json.NewDecoder(r.Body).Decode(gotPayload) // nolint:errcheck

	w.Header().Set("Content-Type", "application/x-ndjson")
	// nolint:errcheck
	_, _ = w.Write([]byte(`{"event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello Zed"}}}
{"status":{"type":"stream_ended"}}
`))
}

func executeZedRequest(t *testing.T, srvURL string) *executor.Result {
	t.Helper()

	ex := executor.GetExecutor("zed")
	if ex == nil {
		t.Fatal("zed executor not registered")
	}

	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:               "",
		AccessToken:          "token-abc",
		RefreshToken:         "",
		BaseURL:              srvURL,
		ProviderSpecificData: map[string]any{"userId": "user-123", "organizationId": "org-456"},
		ProjectID:            "",
	}, "claude-3-5-sonnet", body, true)
	if err != nil {
		t.Fatal(err)
	}

	return res
}

func TestZedExecutor_ExecutionAndStreaming(t *testing.T) {
	var (
		gotLlmTokenReq   bool
		gotCompletionReq bool
		gotAuthHeader    string
		gotPayload       map[string]any
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/llm_tokens" {
			handleMockZedLlmToken(t, w, r, &gotLlmTokenReq)
			return
		}

		if r.URL.Path == "/completions" {
			handleMockZedCompletions(w, r, &gotCompletionReq, &gotAuthHeader, &gotPayload)
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	res := executeZedRequest(t, srv.URL)
	defer func() { _ = res.Body.Close() }() // nolint:errcheck

	if res.StatusCode != 200 || !gotLlmTokenReq || !gotCompletionReq {
		t.Fatalf("status %d, tokenReq=%v, compReq=%v", res.StatusCode, gotLlmTokenReq, gotCompletionReq)
	}

	if gotAuthHeader != "Bearer zed-llm-token-xyz" || gotPayload["provider"] != "Anthropic" {
		t.Errorf("auth=%q provider=%v", gotAuthHeader, gotPayload["provider"])
	}

	outBytes, _ := io.ReadAll(res.Body) // nolint:errcheck
	outStr := string(outBytes)

	if !strings.Contains(outStr, "Hello Zed") || !strings.Contains(outStr, "[DONE]") {
		t.Errorf("unexpected output: %s", outStr)
	}
}
