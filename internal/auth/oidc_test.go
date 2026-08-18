package auth

import (
	"encoding/json"
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestOIDCTest_MissingConfig(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(filepath.Join(dir, "t"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("store close error: %v", err)
		}
	}()

	h := NewOIDCHandler(NewJWTManager("test-secret"), st)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/test", nil)
	rec := httptest.NewRecorder()
	h.Test(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func newOIDCDiscoveryServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "http://" + r.Host,
			"authorization_endpoint": "http://" + r.Host + "/auth",
			"token_endpoint":         "http://" + r.Host + "/token",
			"jwks_uri":               "http://" + r.Host + "/jwks",
		}); err != nil {
			t.Errorf("encode discovery error: %v", err)
		}
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)

		if _, err := w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid authorization code"}`)); err != nil {
			t.Errorf("token write error: %v", err)
		}
	})

	return httptest.NewServer(mux)
}

func setupOIDCStore(t *testing.T, srvURL string) *store.Store {
	t.Helper()

	dir := t.TempDir()

	st, err := store.Open(filepath.Join(dir, "t"))
	if err != nil {
		t.Fatal(err)
	}

	if err := st.SetSetting("oidcIssuerUrl", srvURL); err != nil {
		t.Fatalf("set setting error: %v", err)
	}

	if err := st.SetSetting("oidcClientId", "cid"); err != nil {
		t.Fatalf("set setting error: %v", err)
	}

	if err := st.SetSetting("oidcClientSecret", "sec"); err != nil {
		t.Fatalf("set setting error: %v", err)
	}

	return st
}

func TestOIDCTest_DiscoveryMock(t *testing.T) {
	srv := newOIDCDiscoveryServer(t)
	defer srv.Close()

	st := setupOIDCStore(t, srv.URL)
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("store close error: %v", err)
		}
	}()

	h := NewOIDCHandler(NewJWTManager("test-secret"), st)
	h.client = srv.Client()
	h.allowPrivate = true
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/test", nil)
	req.Host = "localhost:20128"
	rec := httptest.NewRecorder()
	h.Test(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}

	if body["ok"] != true || body["discoveryOk"] != true {
		t.Fatalf("body=%v", body)
	}
}

func TestOIDCStart_NotConfiguredRedirects(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(filepath.Join(dir, "t"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("store close error: %v", err)
		}
	}()

	h := NewOIDCHandler(NewJWTManager("test-secret"), st)
	req := httptest.NewRequest(http.MethodGet, "http://localhost:20128/api/auth/oidc/start", nil)
	rec := httptest.NewRecorder()
	h.Start(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("code=%d", rec.Code)
	}

	loc := rec.Header().Get("Location")
	if loc == "" || !contains(loc, "oidc_not_configured") {
		t.Fatalf("location=%s", loc)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}

func TestCreatePKCE(t *testing.T) {
	v, c, err := createPKCE()
	if err != nil || v == "" || c == "" || v == c {
		t.Fatalf("v=%q c=%q err=%v", v, c, err)
	}
}
