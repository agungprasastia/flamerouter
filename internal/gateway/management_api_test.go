package gateway

import (
	"bytes"
	"encoding/json"
	"flamerouter/internal/auth"
	"flamerouter/internal/config"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testServer(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = st.Close() //nolint:errcheck // test cleanup
	})

	cfg := &config.Config{ //nolint:exhaustruct // test config
		DataDir:       dir,
		JWTSecret:     "test-secret-long-enough",
		APIKeySecret:  "test-api-key-secret",
		MachineIDSalt: "test-salt",
	}
	keys := auth.New(cfg.APIKeySecret)
	// bare Server (no DashboardGuard) — unit-test handlers only
	s := &Server{ //nolint:exhaustruct // test server
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

func testServerForBench(tb testing.TB) (*Server, *store.Store) {
	tb.Helper()
	dir := tb.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() {
		_ = st.Close() //nolint:errcheck // test cleanup
	})

	cfg := &config.Config{ //nolint:exhaustruct // test config
		DataDir:       dir,
		JWTSecret:     "test-secret-long-enough",
		APIKeySecret:  "test-api-key-secret",
		MachineIDSalt: "test-salt",
	}
	keys := auth.New(cfg.APIKeySecret)
	s := &Server{ //nolint:exhaustruct // test server
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

func TestSettingsAPI(t *testing.T) {
	h, _ := testServer(t)

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
}

func TestProxyPoolsAndPricingAPI(t *testing.T) {
	h, st := testServer(t)

	testProxyPoolsFlow(t, h)
	testPricingFlow(t, h)
	testProviderNodesAndCustomModelsFlow(t, h, st)

	req := httptest.NewRequest(http.MethodGet, "/api/init", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("init get %d", rr.Code)
	}
}

func testProxyPoolsFlow(t *testing.T, h http.Handler) {
	body := bytes.NewBufferString(`{"name":"p1","proxyUrl":"http://127.0.0.1:8080"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools", body)
	rr := httptest.NewRecorder()
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
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}

	pools, ok := listed["proxyPools"].([]any)
	if !ok || len(pools) != 1 {
		t.Fatalf("expected 1 pool, got %+v", listed)
	}
}

func testPricingFlow(t *testing.T, h http.Handler) {
	body := bytes.NewBufferString(`{"openai":{"gpt-4o":{"input":1.0,"output":2.0}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/pricing", body)
	rr := httptest.NewRecorder()
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
}

func testProviderNodesAndCustomModelsFlow(t *testing.T, h http.Handler, st *store.Store) {
	body := bytes.NewBufferString(`{"name":"Custom","prefix":"cust","type":"openai-compatible","apiType":"chat","baseUrl":"https://example.com/v1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/provider-nodes", body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("provider node %d %s", rr.Code, rr.Body.String())
	}

	if _, err := st.CreateCustomModel("openai", "my-m", "My", "{}"); err != nil {
		t.Fatal(err)
	}

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
	if err := json.Unmarshal(rr.Body.Bytes(), &testRes); err != nil {
		t.Fatal(err)
	}

	if _, ok := testRes["ok"]; !ok {
		t.Fatalf("expected ok in response, got %+v", testRes)
	}
}

func TestPaginateSliceSafety(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}

	res, total, pages := paginateSlice(items, -10, 2)
	if total != 5 || pages != 3 || len(res) != 2 || res[0] != 1 {
		t.Fatalf("unexpected result for negative page: res=%v total=%d pages=%d", res, total, pages)
	}

	res, total, pages = paginateSlice(items, 1000, 2)
	if total != 5 || pages != 3 || len(res) != 0 {
		t.Fatalf("unexpected result for large page: res=%v total=%d pages=%d", res, total, pages)
	}

	res, total, pages = paginateSlice(items, 1, -5)
	if total != 5 || pages != 5 || len(res) != 1 || res[0] != 1 {
		t.Fatalf("unexpected result for negative pageSize: res=%v total=%d pages=%d", res, total, pages)
	}

	res, total, pages = paginateSlice(items, 1, 10000)
	if total != 5 || pages != 1 || len(res) != 5 {
		t.Fatalf("unexpected result for huge pageSize: res=%v total=%d pages=%d", res, total, pages)
	}
}
