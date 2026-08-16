package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAntigravityMitmAndAliasKV(t *testing.T) {
	h, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cli-tools/antigravity-mitm", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("mitm get %d %s", rr.Code, rr.Body.String())
	}

	var st map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &st)

	if _, ok := st["running"]; !ok {
		t.Fatalf("status: %+v", st)
	}

	// enable DNS for tool
	req = httptest.NewRequest(http.MethodPatch, "/api/cli-tools/antigravity-mitm",
		bytes.NewBufferString(`{"tool":"claude","action":"enable"}`))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("enable dns %d %s", rr.Code, rr.Body.String())
	}

	// alias patch
	req = httptest.NewRequest(http.MethodPatch, "/api/cli-tools/antigravity-mitm/alias",
		bytes.NewBufferString(`{"tool":"claude","mappings":{"a":"gpt-4o"}}`))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("alias %d %s", rr.Code, rr.Body.String())
	}
}

func TestCoworkMCPToolsKV(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPatch, "/api/cli-tools/cowork-mcp-tools",
		bytes.NewBufferString(`{"enabled":true}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("patch %d %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/cli-tools/cowork-mcp-tools", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("get %d", rr.Code)
	}
}

func TestCodexResetCreditsValidation(t *testing.T) {
	h, st := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/usage/missing/codex-reset-credits", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing %d %s", rr.Code, rr.Body.String())
	}

	id, err := st.CreateConnection("openai", "api_key", "o1", "sk", "")
	if err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/usage/"+id+"/codex-reset-credits", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("wrong provider %d %s", rr.Code, rr.Body.String())
	}
}

func TestHeadroomProxyPathRegistered(t *testing.T) {
	h, _ := testServer(t)
	// no headroom running → 502 or 500, but not 404
	req := httptest.NewRequest(http.MethodGet, "/api/headroom/proxy/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatalf("proxy route missing")
	}
}
