package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"flamerouter/internal/ops"
)

func TestTranslatorLoadSave(t *testing.T) {
	h, _ := testServer(t)

	body, _ := json.Marshal(map[string]any{"file": "1_req_client.json", "content": `{"ok":true}`})
	req := httptest.NewRequest(http.MethodPost, "/api/translator/save", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("save status %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/translator/load?file=1_req_client.json", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("load status %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["content"] != `{"ok":true}` {
		t.Fatalf("content=%v", out["content"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/translator/load?file=../../../etc/passwd", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Fatal("expected reject path escape")
	}
}

func TestTranslatorConsoleStream(t *testing.T) {
	h, _ := testServer(t)
	ops.DefaultConsole.Clear()
	ops.DefaultConsole.Append("hello-line")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/translator/console-logs/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream hang")
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("hello-line")) {
		t.Fatalf("expected init logs, got %s", rec.Body.String())
	}
}

func TestListProvidersShape(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if _, ok := out["connections"]; !ok {
		t.Fatalf("want connections key: %v", out)
	}
}

func TestProxyPoolTestNotFound(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/nope/test", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
}
