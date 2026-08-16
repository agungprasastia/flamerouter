package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyDeployValidationAndDryRun(t *testing.T) {
	h, _ := testServer(t)

	// CF missing token
	req := httptest.NewRequest(http.MethodPost, "/api/proxy-pools/cloudflare-deploy",
		bytes.NewBufferString(`{"accountId":"a"}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cf missing %d %s", rr.Code, rr.Body.String())
	}

	// CF dryRun returns script
	req = httptest.NewRequest(http.MethodPost, "/api/proxy-pools/cloudflare-deploy",
		bytes.NewBufferString(`{"dryRun":true,"projectName":"relay-test"}`))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("cf dry %d %s", rr.Code, rr.Body.String())
	}

	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)

	if out["script"] == nil || out["script"] == "" {
		t.Fatalf("script missing: %+v", out)
	}

	// Deno missing org
	req = httptest.NewRequest(http.MethodPost, "/api/proxy-pools/deno-deploy",
		bytes.NewBufferString(`{"denoToken":"t"}`))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("deno org %d", rr.Code)
	}

	// Vercel missing token
	req = httptest.NewRequest(http.MethodPost, "/api/proxy-pools/vercel-deploy",
		bytes.NewBufferString(`{}`))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("vercel token %d", rr.Code)
	}

	// Vercel dryRun
	req = httptest.NewRequest(http.MethodPost, "/api/proxy-pools/vercel-deploy",
		bytes.NewBufferString(`{"dryRun":true}`))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("vercel dry %d %s", rr.Code, rr.Body.String())
	}
}
