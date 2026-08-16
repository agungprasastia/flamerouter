package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/opensse/testutil"
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	t.Cleanup(func() { st.Close() })

	return st
}

func TestModelResolutionExplicitProviderModel(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"hello"}}]}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{})
	if err != nil {
		t.Fatalf("ChatWithOptions err = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}

	if calls[0].Model != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", calls[0].Model)
	}

	if calls[0].Credentials.APIKey != "sk-test" {
		t.Fatalf("apiKey = %q, want sk-test", calls[0].Credentials.APIKey)
	}
}

func TestModelResolutionModelAlias(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	if err := st.SetAlias("fast", "openai/gpt-4o-mini"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-2","choices":[{"message":{"role":"assistant","content":"fast"}}]}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"fast","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{})
	if err != nil {
		t.Fatalf("ChatWithOptions err = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}

	if calls[0].Model != "gpt-4o-mini" {
		t.Fatalf("model = %q, want gpt-4o-mini", calls[0].Model)
	}
}

func TestModelResolutionComboDispatch(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	if _, err := st.CreateCombo("trio", []string{"openai/gpt-4o"}); err != nil {
		t.Fatalf("CreateCombo: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-3","choices":[{"message":{"role":"assistant","content":"combo"}}]}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"trio","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{})
	if err != nil {
		t.Fatalf("ChatWithOptions err = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}

	if calls[0].Model != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", calls[0].Model)
	}
}

func TestModelResolutionMissingProviderReturns400(t *testing.T) {
	st := newTestStore(t)
	fake := testutil.NewFakeExecutor()
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"bare-model-no-provider","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{})
	if err != nil {
		t.Fatalf("ChatWithOptions err = %v", err)
	}

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}

	if res["error"] != "model must be provider/model format" {
		t.Fatalf("error payload = %v", res)
	}

	if len(fake.Calls()) != 0 {
		t.Fatalf("calls = %d, want 0", len(fake.Calls()))
	}
}

func TestModelResolutionProviderAlias(t *testing.T) {
	st := newTestStore(t)
	if _, err := st.CreateConnection("openai", "api_key", "main", "sk-test", ""); err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	fake := testutil.NewFakeExecutor(testutil.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"id":"chatcmpl-4","choices":[{"message":{"role":"assistant","content":"provider-alias"}}]}`),
	})
	fb := fallback.New(st)

	reqBody := []byte(`{"model":"oa/gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	rec := httptest.NewRecorder()

	err := ChatWithOptions(context.Background(), rec, reqBody, st, fake, fb, ChatOptions{})
	if err != nil {
		t.Fatalf("ChatWithOptions err = %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}

	if calls[0].Model != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", calls[0].Model)
	}

	if calls[0].Credentials.APIKey != "sk-test" {
		t.Fatalf("apiKey = %q, want sk-test", calls[0].Credentials.APIKey)
	}
}
