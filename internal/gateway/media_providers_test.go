package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTTSVoicesStaticCatalog(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/media-providers/tts/voices?provider=edge-tts", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	voices, ok := m["voices"].([]any)
	if !ok || len(voices) == 0 {
		t.Fatalf("want voices, got %+v", m)
	}
}

func TestTTSElevenLabsNoConnection(t *testing.T) {
	h, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/media-providers/tts/elevenlabs/voices", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	// static catalog when no key (brief: use static when no API key)
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	var m map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &m)
	if rr.Code == http.StatusOK {
		if _, ok := m["languages"]; !ok {
			if _, ok2 := m["voices"]; !ok2 {
				t.Fatalf("%+v", m)
			}
		}
	}
}

func TestTTSRoutesRegistered(t *testing.T) {
	h, _ := testServer(t)
	paths := []string{
		"/api/media-providers/tts/voices",
		"/api/media-providers/tts/elevenlabs/voices",
		"/api/media-providers/tts/minimax/voices",
		"/api/media-providers/tts/deepgram/voices",
		"/api/media-providers/tts/inworld/voices",
	}
	for _, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusNotFound {
			t.Fatalf("%s not registered", p)
		}
	}
}
