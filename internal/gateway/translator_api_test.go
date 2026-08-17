package gateway

import (
	"bytes"
	"encoding/json"
	"flamerouter/internal/ops"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranslatorTranslateStep1AndConsole(t *testing.T) {
	h, _ := testServer(t)

	testTranslatorStep1(t, h)
	testTranslatorConsoleLogs(t, h)
}

func testTranslatorStep1(t *testing.T, h http.Handler) {
	body := bytes.NewBufferString(`{"step":1,"body":{"model":"openai/gpt-4o","messages":[{"role":"user","content":"hi"}]}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/translator/translate", body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("translate step1 %d %s", rr.Code, rr.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	if out["success"] != true {
		t.Fatalf("success: %+v", out)
	}

	res, ok := out["result"].(map[string]any)
	if !ok || res["provider"] != "openai" || res["sourceFormat"] != "openai" {
		t.Fatalf("result: %+v", res)
	}
}

func testTranslatorConsoleLogs(t *testing.T, h http.Handler) {
	ops.DefaultConsole.Clear()
	ops.DefaultConsole.Append("line-1")

	req := httptest.NewRequest(http.MethodGet, "/api/translator/console-logs", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("console get %d", rr.Code)
	}

	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}

	logs, ok := out["logs"].([]any)
	if !ok || len(logs) != 1 || logs[0] != "line-1" {
		t.Fatalf("logs: %+v", out)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/translator/console-logs", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("console delete %d", rr.Code)
	}

	if n := len(ops.DefaultConsole.Get()); n != 0 {
		t.Fatalf("expected empty, got %d", n)
	}
}

func TestTranslatorSendValidation(t *testing.T) {
	h, _ := testServer(t)
	body := bytes.NewBufferString(`{"provider":"openai","model":"gpt-4o"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/translator/send", body)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 got %d %s", rr.Code, rr.Body.String())
	}
}
