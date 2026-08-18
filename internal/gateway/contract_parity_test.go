package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContractParity_Combos(t *testing.T) {
	h, st := testServer(t)

	id, err := st.CreateCombo("test-combo", []string{"gpt-4o", "claude-3-5-sonnet"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. GET /api/combos -> {"combos": [...]}
	req := httptest.NewRequest(http.MethodGet, "/api/combos", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/combos code=%d", rec.Code)
	}

	var listResp struct {
		Combos []struct {
			ID     string   `json:"id"`
			Name   string   `json:"name"`
			Models []string `json:"models"`
		} `json:"combos"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal combos list error: %v", err)
	}

	if len(listResp.Combos) != 1 || listResp.Combos[0].ID != id {
		t.Fatalf("unexpected combos list: %+v", listResp)
	}

	// 2. GET /api/combos/{id} -> Single Combo Object
	req = httptest.NewRequest(http.MethodGet, "/api/combos/"+id, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/combos/%s code=%d", id, rec.Code)
	}

	var singleResp struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Models []string `json:"models"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &singleResp); err != nil {
		t.Fatalf("unmarshal single combo error: %v", err)
	}

	if singleResp.ID != id || singleResp.Name != "test-combo" || len(singleResp.Models) != 2 {
		t.Fatalf("unexpected single combo: %+v", singleResp)
	}
}

func TestContractParity_PxpipeHealth(t *testing.T) {
	h, _ := testServer(t)

	// POST /api/pxpipe/health
	req := httptest.NewRequest(http.MethodPost, "/api/pxpipe/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/pxpipe/health code=%d, body=%s", rec.Code, rec.Body.String())
	}

	var postResp struct {
		Status  string `json:"status"`
		Healthy bool   `json:"healthy"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &postResp); err != nil {
		t.Fatalf("unmarshal pxpipe health error: %v", err)
	}

	// GET /api/pxpipe/health
	req = httptest.NewRequest(http.MethodGet, "/api/pxpipe/health", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/pxpipe/health code=%d", rec.Code)
	}
}

func TestContractParity_HeadroomStatus(t *testing.T) {
	h, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/headroom/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/headroom/status code=%d", rec.Code)
	}

	var statusResp struct {
		Extras    map[string]bool `json:"extras"`
		Installed bool            `json:"installed"`
		Running   bool            `json:"running"`
		LocalURL  bool            `json:"localUrl"`
		CanStart  bool            `json:"canStart"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("unmarshal headroom status error: %v", err)
	}

	if statusResp.Extras == nil {
		t.Fatalf("expected non-nil extras map")
	}
}

func TestContractParity_SettingsNumbersAndJSON(t *testing.T) {
	h, st := testServer(t)

	if err := st.SetSetting("pxpipeMinChars", "30000"); err != nil {
		t.Fatal(err)
	}

	if err := st.SetSetting("comboStrategies", `{"my-combo":{"fallbackStrategy":"round-robin"}}`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/settings code=%d", rec.Code)
	}

	var settings map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}

	// pxpipeMinChars should be number (float64 in unmarshaled json)
	val, ok := settings["pxpipeMinChars"].(float64)
	if !ok || val != 30000 {
		t.Fatalf("expected pxpipeMinChars=30000 number, got %T (%v)", settings["pxpipeMinChars"], settings["pxpipeMinChars"])
	}

	// comboStrategies should be unmarshaled map/object
	strategies, ok := settings["comboStrategies"].(map[string]any)
	if !ok {
		t.Fatalf("expected comboStrategies object, got %T (%v)", settings["comboStrategies"], settings["comboStrategies"])
	}

	if _, ok := strategies["my-combo"]; !ok {
		t.Fatalf("expected my-combo inside comboStrategies, got %+v", strategies)
	}
}
