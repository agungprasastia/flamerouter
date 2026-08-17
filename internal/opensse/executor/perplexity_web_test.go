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

func TestPerplexityWebExecutor_ExecutionAndStreaming(t *testing.T) {
	var gotPayload map[string]any

	var gotCookie string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		defer func() { _ = r.Body.Close() }()
		_ = json.NewDecoder(r.Body).Decode(&gotPayload) // nolint:errcheck

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"blocks":[{"intended_usage":"pro_search_steps","plan_block":{"steps":[{"step_type":"SEARCH_WEB","search_web_content":{"queries":[{"query":"golang test"}]}}]}}]}

data: {"backend_uuid":"uuid-12345","blocks":[{"intended_usage":"markdown","markdown_block":{"chunks":["Hello from Perplexity"],"progress":"DONE"}}],"final":true}

`)) // nolint:errcheck
	}))
	defer srv.Close()

	ex := executor.GetExecutor("perplexity-web")
	if ex == nil {
		t.Fatal("perplexity-web executor not registered")
	}

	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]string{{"role": "user", "content": "test question"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:               "session-token-xyz",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              srv.URL,
		ProviderSpecificData: nil,
		ProjectID:            "",
	}, "pplx-sonar", body, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	if !strings.Contains(gotCookie, "__Secure-next-auth.session-token=session-token-xyz") {
		t.Errorf("got cookie %q, want session token", gotCookie)
	}

	outBytes, _ := io.ReadAll(res.Body) // nolint:errcheck
	outStr := string(outBytes)

	if !strings.Contains(outStr, "Searching: golang test") {
		t.Errorf("stream missing thinking reasoning_content: %s", outStr)
	}

	if !strings.Contains(outStr, "Hello from Perplexity") {
		t.Errorf("stream missing content: %s", outStr)
	}

	if !strings.Contains(outStr, "[DONE]") {
		t.Errorf("stream missing [DONE]: %s", outStr)
	}
}

func TestPerplexityWebExecutor_NonStreaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"backend_uuid":"uuid-999","blocks":[{"intended_usage":"markdown","markdown_block":{"chunks":["Direct answer"],"progress":"DONE"}}],"final":true}
`)) // nolint:errcheck
	}))
	defer srv.Close()

	ex := executor.GetExecutor("perplexity-web")
	body, _ := json.Marshal(map[string]any{ // nolint:errcheck
		"messages": []map[string]string{{"role": "user", "content": "direct query"}},
	})

	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:               "",
		AccessToken:          "jwt-token-123",
		RefreshToken:         "",
		BaseURL:              srv.URL,
		ProviderSpecificData: nil,
		ProjectID:            "",
	}, "pplx-gpt", body, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

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
	if !ok || msg["content"] != "Direct answer" {
		t.Errorf("content = %v, want Direct answer", msg["content"])
	}
}
