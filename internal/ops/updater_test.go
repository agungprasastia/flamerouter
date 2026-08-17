package ops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.2.3"}); err != nil {
			t.Errorf("encode error: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	old := ReleaseURL
	ReleaseURL = srv.URL

	t.Cleanup(func() { ReleaseURL = old })

	Version = "1.0.0"

	cur, latest, avail, err := CheckVersion()
	if err != nil {
		t.Fatal(err)
	}

	if cur != "1.0.0" || latest != "1.2.3" || !avail {
		t.Fatalf("got cur=%s latest=%s avail=%v", cur, latest, avail)
	}
}

func TestShutdownNil(t *testing.T) {
	if err := Shutdown(nil); err != nil {
		t.Fatal(err)
	}
}
