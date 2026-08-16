package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"flamerouter/internal/auth"
	"flamerouter/internal/config"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
)

func testServer(t *testing.T) (http.Handler, *store.Store) {
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
	// bare Server (no DashboardGuard) — unit-test handlers only
	s := &Server{
		cfg:     cfg,
		st:      st,
		keys:    keys,
		fb:      fallback.New(st),
		jwt:     auth.NewJWTManager(cfg.JWTSecret),
		session: auth.NewSessionHandler(auth.NewJWTManager(cfg.JWTSecret), st, "123456"),
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s, st
}

func TestSettingsAndProxyPoolsAPI(t *testing.T) {
	h, st := testServer(t)

	// patch settings
	body := bytes.NewBufferString(`{"requireLogin":"false","comboStrategy":"fallback"}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/settings", body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH settings status %d body %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET settings %d", rr.Code)
	}
	var settings map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["comboStrategy"] != "fallback" {
		t.Fatalf("settings: %+v", settings)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings/require-login", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("require-login %d", rr.Code)
	}

	// proxy pool create + list
	body = bytes.NewBufferString(`{"name":"p1","proxyUrl":"http://127.0.0.1:8080"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/proxy-pools", body)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create pool %d %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/proxy-pools", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list pools %d", rr.Code)
	}
	var listed map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &listed)
	pools, _ := listed["proxyPools"].([]any)
	if len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %+v", listed)
	}

	// pricing
	body = bytes.NewBufferString(`{"openai":{"gpt-4o":{"input":1.0,"output":2.0}}}`)
	req = httptest.NewRequest(http.MethodPost, "/api/pricing", body)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("pricing post %d %s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("pricing get %d", rr.Code)
	}

	// provider node
	body = bytes.NewBufferString(`{"name":"Custom","prefix":"cust","type":"openai-compatible","apiType":"chat","baseUrl":"https://example.com/v1"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/provider-nodes", body)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("provider node %d %s", rr.Code, rr.Body.String())
	}

	// custom model via store path
	_, _ = st.CreateCustomModel("openai", "my-m", "My", "{}")
	req = httptest.NewRequest(http.MethodGet, "/api/models/custom", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("custom models %d", rr.Code)
	}

	body = bytes.NewBufferString(`{"model":"oc/laguna-s-2.1-free"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/models/test", body)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("model test post %d %s", rr.Code, rr.Body.String())
	}
	var testRes map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &testRes)
	if testRes["ok"] != true {
		t.Fatalf("expected test ok=true, got %+v", testRes)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/init", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "Initialized" {
		t.Fatalf("init %d %q", rr.Code, rr.Body.String())
	}
}
