package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestXiaomiTokenplan_BuildURL(t *testing.T) {
	// 1. Default OpenAI format (sgp default)
	cred1 := Credentials{}

	url1 := buildXiaomiTokenplanURL("mimo-v2.5-pro", true, cred1)
	if url1 != "https://token-plan-sgp.xiaomimimo.com/v1/chat/completions" {
		t.Fatalf("expected sgp openai url, got %s", url1)
	}

	// 2. Region cn
	cred2 := Credentials{
		ProviderSpecificData: map[string]any{"region": "cn"},
	}

	url2 := buildXiaomiTokenplanURL("mimo-v2.5-pro", true, cred2)
	if url2 != "https://token-plan-cn.xiaomimimo.com/v1/chat/completions" {
		t.Fatalf("expected cn openai url, got %s", url2)
	}

	// 3. Claude format via runtimeTransport
	cred3 := Credentials{
		ProviderSpecificData: map[string]any{
			"region": "ams",
			"runtimeTransport": map[string]any{
				"format": "claude",
			},
		},
	}

	url3 := buildXiaomiTokenplanURL("mimo-v2.5-pro", true, cred3)
	if url3 != "https://token-plan-ams.xiaomimimo.com/anthropic/v1/messages" {
		t.Fatalf("expected ams claude url, got %s", url3)
	}

	// 4. Claude format via model suffix
	cred4 := Credentials{
		ProviderSpecificData: map[string]any{"region": "sgp"},
	}

	url4 := buildXiaomiTokenplanURL("mimo-v2.5-pro-claude", true, cred4)
	if url4 != "https://token-plan-sgp.xiaomimimo.com/anthropic/v1/messages" {
		t.Fatalf("expected sgp claude url via model, got %s", url4)
	}
}

func TestXiaomiTokenplan_ExecuteMock(t *testing.T) {
	var requestedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	defer srv.Close()

	ex := NewXiaomiTokenplanExecutor(srv.Client())
	cred := Credentials{
		APIKey:  "tp-secret",
		BaseURL: srv.URL + "/anthropic/v1/messages",
	}

	res, err := ex.Execute(context.Background(), cred, "mimo-v2.5-pro-claude", []byte(`{"messages":[]}`), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	if requestedPath != "/anthropic/v1/messages" {
		t.Fatalf("expected /anthropic/v1/messages, got %s", requestedPath)
	}

	_, _ = io.ReadAll(res.Body)
}
