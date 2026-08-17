package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMimoFree_InjectSystemMarker(t *testing.T) {
	body := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "Hi"},
		},
	}
	res := injectMimoSystemMarker(body)

	msgs, ok := res["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	sys, okSys := msgs[0].(map[string]any)
	if !okSys || sys["role"] != "system" || sys["content"] != MimoSystemMarker {
		t.Fatalf("expected system marker, got %v", sys)
	}

	// Idempotent check
	res2 := injectMimoSystemMarker(res)

	msgs2, ok2 := res2["messages"].([]any)
	if !ok2 || len(msgs2) != 2 {
		t.Fatalf("expected idempotent 2 messages, got %d", len(msgs2))
	}
}

func TestMimoFree_SessionID(t *testing.T) {
	id := generateMimoSessionID()
	if !strings.HasPrefix(id, MimoSessionAffinity) {
		t.Fatalf("expected prefix %s, got %s", MimoSessionAffinity, id)
	}

	if len(id) != len(MimoSessionAffinity)+MimoSessionIDLength {
		t.Fatalf("expected length %d, got %d", len(MimoSessionAffinity)+MimoSessionIDLength, len(id))
	}
}

func TestMimoFree_ParseJWTExp(t *testing.T) {
	now := time.Now().Unix()
	payload := map[string]any{"exp": now + 3600}

	pb, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	fakeJWT := "header." + base64.RawURLEncoding.EncodeToString(pb) + ".sig"

	exp := parseMimoJWTExp(fakeJWT)
	if exp != (now+3600)*1000 {
		t.Fatalf("expected exp %d, got %d", (now+3600)*1000, exp)
	}
}

func handleMimoTestBootstrap(w http.ResponseWriter, bootstrapCount *int32) {
	atomic.AddInt32(bootstrapCount, 1)
	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(map[string]any{ // nolint:errcheck
		"jwt": "fake.jwt.token",
	})
}

func handleMimoTestChat(t *testing.T, w http.ResponseWriter, r *http.Request, chatCount *int32) {
	t.Helper()

	c := atomic.AddInt32(chatCount, 1)
	if c == 1 {
		// Simulate 401 on first try
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`)) // nolint:errcheck

		return
	}
	// Second try succeeds
	if r.Header.Get("X-Mimo-Source") != "mimocode-cli-free" {
		t.Errorf("missing X-Mimo-Source header")
	}

	if r.Header.Get("x-session-affinity") == "" {
		t.Errorf("missing x-session-affinity header")
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"response"}}]}`)) // nolint:errcheck
}

func TestMimoFree_ExecuteWithBootstrapAndRetry(t *testing.T) {
	var bootstrapCount int32

	var chatCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/free-ai/bootstrap" {
			handleMimoTestBootstrap(w, &bootstrapCount)
			return
		}

		if r.URL.Path == "/v1/chat/completions" {
			handleMimoTestChat(t, w, r, &chatCount)
			return
		}

		http.NotFound(w, r)
	}))
	defer srv.Close()

	ex := NewMimoFreeExecutor(srv.Client())
	ex.bootstrapURL = srv.URL + "/api/free-ai/bootstrap"
	ex.chatURL = srv.URL + "/v1/chat/completions"

	body := []byte(`{"messages":[{"role":"user","content":"Hello"}]}`)

	res, err := ex.Execute(context.Background(), Credentials{
		ProviderSpecificData: nil,
		APIKey:               "",
		AccessToken:          "",
		RefreshToken:         "",
		BaseURL:              "",
		ProjectID:            "",
	}, "mimo-free", body, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	defer func() {
		if cErr := res.Body.Close(); cErr != nil {
			t.Errorf("closing body: %v", cErr)
		}
	}()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retry, got %d", res.StatusCode)
	}

	if atomic.LoadInt32(&bootstrapCount) != 2 {
		t.Fatalf("expected 2 bootstrap calls (initial + retry), got %d", atomic.LoadInt32(&bootstrapCount))
	}

	if atomic.LoadInt32(&chatCount) != 2 {
		t.Fatalf("expected 2 chat calls, got %d", atomic.LoadInt32(&chatCount))
	}

	b, err := io.ReadAll(res.Body)
	if err != nil || !strings.Contains(string(b), "response") {
		t.Fatalf("expected body response, got %s (err: %v)", string(b), err)
	}
}
