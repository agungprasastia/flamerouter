package auth

import (
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDashboardGuardRequireLoginPublicGET(t *testing.T) {
	jwt := NewJWTManager("test-secret-long-enough-for-hs256")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if _, err := w.Write([]byte(`{"requireLogin":true}`)); err != nil {
			t.Errorf("write response error: %v", err)
		}
	})
	h := DashboardGuard(jwt, (*store.Store)(nil), inner)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/require-login", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET require-login unauth: want 200 got %d body %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/api/settings/require-login", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("PATCH require-login unauth: want 401 got %d", rr.Code)
	}
}
