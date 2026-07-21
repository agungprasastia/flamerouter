package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Critical backend paths must be registered (not 404).
// Full 9router diff documented in .superpowers/sdd/full-parity-task-15-report.md.
func TestRouteTable_CriticalPathsRegistered(t *testing.T) {
	h, _ := testServer(t)

	// method, path — expect not 404 (auth/method errors OK)
	cases := []struct {
		method string
		path   string
	}{
		{"GET", "/api/health"},
		{"POST", "/api/auth/login"},
		{"GET", "/api/auth/status"},
		{"GET", "/api/settings"},
		{"GET", "/api/settings/require-login"},
		{"PATCH", "/api/settings/require-login"},
		{"PATCH", "/api/pricing"},
		{"DELETE", "/api/pricing"},
		{"POST", "/api/locale"},
		{"GET", "/api/oauth/codex/start-proxy"},
		{"GET", "/api/oauth/codex/poll-status"},
		{"GET", "/api/oauth/codex/stop-proxy"},
		{"POST", "/api/oauth/xai/exchange"},
		{"POST", "/api/oauth/xai/manual-code"},
		{"GET", "/api/oauth/github/device-code"},
		{"GET", "/api/version"},
		{"POST", "/api/shutdown"},
		{"POST", "/api/version/shutdown"},
		{"GET", "/api/usage/stream"},
		{"GET", "/api/usage/stats"},
		{"GET", "/v1/models"},
		{"GET", "/v1/models/info"},
		{"POST", "/v1/chat/completions"},
		{"POST", "/v1/messages"},
		{"POST", "/v1/responses"},
		{"POST", "/v1/embeddings"},
		{"POST", "/v1/images/generations"},
		{"POST", "/v1/audio/speech"},
		{"POST", "/v1/audio/transcriptions"},
		{"GET", "/v1/audio/voices"},
		// POST /v1/videos/* needs Fallback in full New(); registration covered by build
		{"POST", "/v1/search"},
		{"POST", "/v1/web/fetch"},
		{"POST", "/v1/messages/count_tokens"},
		{"POST", "/v1/api/chat"},
		{"GET", "/v1beta/models"},
		{"POST", "/api/providers/connections"},
		{"GET", "/api/providers/client"},
		{"GET", "/api/proxy-pools"},
		{"GET", "/api/combos"},
		{"GET", "/api/keys"},
		{"GET", "/api/tunnel/status"},
		{"GET", "/api/mitm/status"},
		{"GET", "/api/headroom/status"},
		{"GET", "/api/pxpipe/status"},
		{"POST", "/api/translator/translate"},
		{"GET", "/api/translator/load"},
		{"POST", "/api/translator/save"},
		// console-logs/stream is long-lived SSE — covered by TestTranslatorConsoleStream
		// proxy-pools/{id}/test returns 404 for missing id — covered by TestProxyPoolTestNotFound
		{"GET", "/api/providers"},
		{"GET", "/api/models"},
		{"GET", "/api/pricing"},
		{"GET", "/api/tags"},
		{"GET", "/api/locale"},
		{"GET", "/api/init"},
	}

	for _, tc := range cases {
		t.Run(tc.method+"_"+strings.ReplaceAll(tc.path, "/", "_"), func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			if tc.method == http.MethodPost || tc.method == http.MethodPatch {
				req.Header.Set("Content-Type", "application/json")
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code == http.StatusNotFound {
				t.Fatalf("%s %s → 404 (route missing)", tc.method, tc.path)
			}
		})
	}
}

func TestRouteTable_ShutdownSecret(t *testing.T) {
	h, _ := testServer(t)
	t.Setenv("SHUTDOWN_SECRET", "test-shutdown")

	req := httptest.NewRequest(http.MethodPost, "/api/shutdown", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("no secret: status %d want 401", rr.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/shutdown", nil)
	req.Header.Set("Authorization", "Bearer test-shutdown")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	// may 200 before process shutdown goroutine; not 401/404
	if rr.Code == http.StatusUnauthorized || rr.Code == http.StatusNotFound {
		t.Fatalf("with secret: status %d body %s", rr.Code, rr.Body.String())
	}
}
