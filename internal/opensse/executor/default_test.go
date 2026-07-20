package executor_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"flamerouter/internal/opensse/executor"
)

func TestDefaultExecutor_ForwardsChat(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","choices":[{"message":{"role":"assistant","content":"hi"}}]}`))
	}))
	defer srv.Close()

	ex := executor.NewDefault(srv.Client())
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hey"}},
	})
	res, err := ex.Execute(context.Background(), executor.Credentials{
		APIKey:  "sk-test",
		BaseURL: srv.URL + "/v1",
	}, "gpt-4o-mini", body, false)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth %q", gotAuth)
	}
	if gotBody["model"] != "gpt-4o-mini" {
		t.Fatalf("model field %+v", gotBody["model"])
	}
	b, _ := io.ReadAll(res.Body)
	if len(b) == 0 {
		t.Fatal("empty body")
	}
}
