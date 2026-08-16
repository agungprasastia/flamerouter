package handlers

import (
	"context"
	"encoding/json"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func videoTestStore(t *testing.T) *store.Store {
	t.Helper()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	return st
}

func TestVideoExtensionsAction(t *testing.T) {
	st := videoTestStore(t)

	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"r1","status":"pending"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := st.CreateConnection("xai", "api_key", "x", "sk-test", srv.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}

	fb := fallback.New(st)

	body := []byte(`{"model":"xai/grok-imagine-video","prompt":"extend"}`)
	req := httptest.NewRequest(http.MethodPost, "http://localhost/v1/videos/extensions", nil)
	rr := httptest.NewRecorder()
	_ = Video(context.Background(), rr, req, body, st, nil, fb)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}

	if !strings.HasSuffix(gotPath, "/videos/extensions") {
		t.Fatalf("upstream path %q", gotPath)
	}

	if rr.Header().Get("x-9router-connection-id") == "" {
		t.Fatal("missing connection header")
	}
}

func TestVideoPoll(t *testing.T) {
	st := videoTestStore(t)

	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		if r.Method != http.MethodGet {
			t.Errorf("method %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"job-1","status":"done"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := st.CreateConnection("xai", "api_key", "x", "sk-test", srv.URL+"/v1")
	if err != nil {
		t.Fatal(err)
	}

	fb := fallback.New(st)

	req := httptest.NewRequest(http.MethodGet, "http://localhost/v1/videos/job-1", nil)
	rr := httptest.NewRecorder()
	_ = VideoPoll(context.Background(), rr, req, "job-1", st, fb)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}

	if !strings.HasSuffix(gotPath, "/videos/job-1") {
		t.Fatalf("upstream path %q", gotPath)
	}

	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}

	if m["status"] != "done" {
		t.Fatalf("%+v", m)
	}
}

func TestVideoPollMissingID(t *testing.T) {
	st := videoTestStore(t)
	fb := fallback.New(st)
	req := httptest.NewRequest(http.MethodGet, "http://localhost/v1/videos/", nil)
	rr := httptest.NewRecorder()
	_ = VideoPoll(context.Background(), rr, req, "", st, fb)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d %s", rr.Code, rr.Body.String())
	}
}

func TestVideoPollNoCredentials(t *testing.T) {
	st := videoTestStore(t)
	fb := fallback.New(st)
	req := httptest.NewRequest(http.MethodGet, "http://localhost/v1/videos/job-1", nil)
	rr := httptest.NewRecorder()
	_ = VideoPoll(context.Background(), rr, req, "job-1", st, fb)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d %s", rr.Code, rr.Body.String())
	}
}
