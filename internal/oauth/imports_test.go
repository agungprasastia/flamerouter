package oauth

import (
	"bytes"
	"encoding/json"
	"flamerouter/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("failed to close store: %v", err)
		}
	})

	return st
}

func TestSpecializedImport_CodexToken(t *testing.T) {
	st := testStore(t)
	h := NewHandler()
	body := `{"accessToken":"tok-123","name":"c1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/codex/import-token", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	if !h.SpecializedImport(rr, req, "codex/import-token", st) {
		t.Fatal("not handled")
	}

	if rr.Code != 200 {
		t.Fatalf("code %d body %s", rr.Code, rr.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if out["success"] != true {
		t.Fatalf("resp %v", out)
	}

	conns, err := st.ListConnectionsByProvider("codex")
	if err != nil || len(conns) != 1 {
		t.Fatalf("conns %v %v", conns, err)
	}

	if conns[0].AccessToken != "tok-123" {
		t.Fatalf("token %q", conns[0].AccessToken)
	}
}

func TestSpecializedImport_GitLabPAT(t *testing.T) {
	st := testStore(t)
	h := NewHandler()
	body := `{"token":"glpat-x","baseUrl":"https://gitlab.example"}`
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/gitlab/pat", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	if !h.SpecializedImport(rr, req, "gitlab/pat", st) {
		t.Fatal("not handled")
	}

	if rr.Code != 200 {
		t.Fatalf("code %d %s", rr.Code, rr.Body.String())
	}

	conns, err := st.ListConnectionsByProvider("gitlab")
	if err != nil || len(conns) != 1 || conns[0].AccessToken != "glpat-x" {
		t.Fatalf("%+v (err=%v)", conns, err)
	}
}

func TestSpecializedImport_Unknown(t *testing.T) {
	h := NewHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/oauth/x/y", nil)
	rr := httptest.NewRecorder()

	if h.SpecializedImport(rr, req, "x/y", nil) {
		t.Fatal("should not handle")
	}
}
