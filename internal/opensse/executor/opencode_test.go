package executor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func newOpenCodeMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-opencode-request") == "" {
			t.Errorf("missing x-opencode-request header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if _, writeErr := w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}]}`)); writeErr != nil {
			_ = writeErr
		}
	}))
}

func TestOpenCodeExecutorMock(t *testing.T) {
	t.Parallel()

	srv := newOpenCodeMockServer(t)
	defer srv.Close()

	ex := &OpenCodeExecutor{
		Base: Base{
			Provider: "opencode",
			Client:   nil,
			Headers:  nil,
			BaseURL:  srv.URL,
			BaseURLs: nil,
		},
	}

	body, err := json.Marshal(map[string]any{
		"model": "laguna-s-2.1-free",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	cred := Credentials{
		ProviderSpecificData: nil,
		APIKey:               "",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              srv.URL,
		ProjectID:            "",
	}

	res, err := ex.Execute(context.Background(), cred, "laguna-s-2.1-free", body, false)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	respBytes, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		t.Fatalf("read error: %v", readErr)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	if string(respBytes) != `{"choices":[{"message":{"content":"pong"}}]}` {
		t.Fatalf("unexpected response body: %s", string(respBytes))
	}
}

func TestOpenCodeExecutorLive(t *testing.T) {
	if testing.Short() || os.Getenv("OPENCODE_LIVE_TEST") == "" {
		t.Skip("skipping live network test unless OPENCODE_LIVE_TEST is set")
	}

	ex := GetExecutor("opencode")
	if ex == nil {
		t.Fatal("opencode executor not found")
	}

	body, err := json.Marshal(map[string]any{
		"model": "laguna-s-2.1-free",
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	cred := Credentials{
		ProviderSpecificData: nil,
		APIKey:               "",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              "",
		ProjectID:            "",
	}

	res, err := ex.Execute(context.Background(), cred, "laguna-s-2.1-free", body, false)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			_ = err
		}
	}()

	respBytes, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		_ = readErr
	}

	t.Logf("Status: %d, Response: %s", res.StatusCode, string(respBytes))

	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
}
