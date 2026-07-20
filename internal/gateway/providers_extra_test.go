package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProvidersClientRoute(t *testing.T) {
	h, st := testServer(t)
	_, err := st.CreateConnection("openai", "api_key", "main", "sk-test", "")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/providers/client", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["connections"]; !ok {
		t.Fatalf("%+v", m)
	}
	// no secrets
	raw := rr.Body.String()
	if bytes.Contains([]byte(raw), []byte("sk-test")) {
		t.Fatal("api key leaked")
	}
}

func TestSuggestedModelsMissingParams(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/providers/suggested-models", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", rr.Code)
	}
}

func TestSuggestedModelsUnknownFilter(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/providers/suggested-models?url=http://example.com&type=nope", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d", rr.Code)
	}
}

func TestTestBatchByIDs(t *testing.T) {
	h, st := testServer(t)
	id, err := st.CreateConnection("openai", "api_key", "main", "sk-test", "")
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"ids":["` + id + `"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/providers/test-batch", body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	results, _ := m["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("%+v", m)
	}
	r0 := results[0].(map[string]any)
	if r0["valid"] != true {
		t.Fatalf("%+v", r0)
	}
}

func TestKiloFreeModelsRoute(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/providers/kilo/free-models", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	// live fetch or empty list / 502 — must not 404
	if rr.Code == http.StatusNotFound {
		t.Fatal("route missing")
	}
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if _, ok := m["models"]; !ok && rr.Code == http.StatusOK {
		t.Fatalf("%+v", m)
	}
}

func TestStaticProviderPathsBeforeID(t *testing.T) {
	h, _ := testServer(t)
	// client must not be captured as {id}
	req := httptest.NewRequest(http.MethodGet, "/api/providers/client", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code == http.StatusNotFound {
		t.Fatal("client 404")
	}
	// {id} still works
	req = httptest.NewRequest(http.MethodGet, "/api/providers/openai", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("id route %d", rr.Code)
	}
}
