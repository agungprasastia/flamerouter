package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleKeys_GET_ListsKeys(t *testing.T) {
	h, st := testServer(t)
	// seed one key via store
	_, err := st.CreateAPIKey("t", "kid1", "hash1", "mid1")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}

	var res struct {
		Keys []map[string]any `json:"keys"`
	}

	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}

	if len(res.Keys) < 1 {
		t.Fatal("expected >=1 key")
	}
}
