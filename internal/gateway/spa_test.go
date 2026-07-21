package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPA_ServesIndex(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "html") && !strings.Contains(rr.Body.String(), "FlameRouter") {
		t.Fatalf("body %q", rr.Body.String())
	}
}

func TestSPA_DoesNotStealAPI(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == 404 {
		t.Fatal("api health stolen")
	}
	if !strings.Contains(rr.Body.String(), "ok") {
		t.Fatalf("body %s", rr.Body.String())
	}
}
