package executor_test

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/executor"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultExecutor_ForwardsChat(t *testing.T) {
	var gotAuth string

	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")

		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}

		defer func() {
			_ = r.Body.Close() // nolint:errcheck
		}()

		_ = json.NewDecoder(r.Body).Decode(&gotBody) // nolint:errcheck

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)) // nolint:errcheck
	}))
	defer srv.Close()

	ex := executor.NewDefault(srv.Client())

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hey"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:               "sk-test",
		BaseURL:              srv.URL + "/v1",
		ProviderSpecificData: nil,
		AccessToken:          "",
		RefreshToken:         "",
		ProjectID:            "",
	}, "gpt-4o-mini", body, false)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = res.Body.Close() // nolint:errcheck
	}()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}

	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth %q", gotAuth)
	}

	if gotBody["model"] != "gpt-4o-mini" {
		t.Fatalf("model field %+v", gotBody["model"])
	}

	b, err := io.ReadAll(res.Body)
	if err != nil || len(b) == 0 {
		t.Fatal("empty body or read error")
	}
}

func TestDefaultExecutor_NullBodyNoPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1"}`)) // nolint:errcheck
	}))
	defer srv.Close()

	ex := executor.NewDefault(srv.Client())
	creds := executor.Credentials{BaseURL: srv.URL + "/v1"} //nolint:exhaustruct // test fixture

	res, err := ex.Execute(context.Background(), creds, "gpt-4o", []byte("null"), false)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		_ = res.Body.Close() // nolint:errcheck
	}()

	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
}
