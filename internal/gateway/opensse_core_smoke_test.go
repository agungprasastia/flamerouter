package gateway

import (
	"bytes"
	"encoding/json"
	"flamerouter/internal/auth"
	"flamerouter/internal/config"
	"flamerouter/internal/opensse/testutil"
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testOpenSSEServer(t *testing.T, fakeExec *testutil.FakeExecutor) (http.Handler, *store.Store) {
	t.Helper()
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{
		DataDir:       dir,
		JWTSecret:     "test-secret-long-enough",
		APIKeySecret:  "test-api-key-secret",
		MachineIDSalt: "test-salt",
	}
	keys := auth.New(cfg.APIKeySecret)
	handler := New(cfg, st, keys, fakeExec)

	return handler, st
}

func TestOpenSSECoreSmokeChatCompletions(t *testing.T) {
	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-smoke-1","choices":[{"message":{"role":"assistant","content":"smoke chat"}}]}`),
	})

	h, st := testOpenSSEServer(t, fake)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatal(err)
	}

	reqBody := bytes.NewBufferString(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}

	if len(fake.Calls()) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.Calls()))
	}
}

func TestOpenSSECoreSmokeMessages(t *testing.T) {
	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-smoke-2","choices":[{"message":{"role":"assistant","content":"smoke message"}}]}`),
	})

	h, st := testOpenSSEServer(t, fake)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatal(err)
	}

	reqBody := bytes.NewBufferString(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hello claude"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body.String())
	}

	if len(fake.Calls()) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.Calls()))
	}
}

func TestOpenSSECoreSmokeResponses(t *testing.T) {
	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-smoke-3","choices":[{"message":{"role":"assistant","content":"smoke responses"}}]}`),
	})

	h, st := testOpenSSEServer(t, fake)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatal(err)
	}

	reqBody := bytes.NewBufferString(`{"model":"openai/gpt-4o","input":"smoke input"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body.String())
	}

	if len(fake.Calls()) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.Calls()))
	}
}

func TestOpenSSECoreSmokeGeminiModels(t *testing.T) {
	fake := testutil.NewFakeExecutor()
	h, _ := testOpenSSEServer(t, fake)

	req := httptest.NewRequest(http.MethodGet, "/v1beta/models", nil)
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body.String())
	}

	var res map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}

	if _, ok := res["models"]; !ok {
		t.Fatalf("missing models in response: %v", res)
	}
}

func TestOpenSSECoreSmokeVercelAIChat(t *testing.T) {
	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: []byte(`{
			"id":"chatcmpl-smoke-4",
			"choices":[{"message":{"role":"assistant","content":"smoke vercel"}}],
			"usage":{"prompt_tokens":5,"completion_tokens":10}
		}`),
	})

	h, st := testOpenSSEServer(t, fake)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatal(err)
	}

	reqBody := bytes.NewBufferString(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"smoke vercel"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/api/chat", reqBody)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body.String())
	}

	if len(fake.Calls()) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.Calls()))
	}
}
