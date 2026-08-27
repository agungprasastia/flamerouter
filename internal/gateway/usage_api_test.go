package gateway

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"flamerouter/internal/store"
)

func TestExtractConnUsageData(t *testing.T) {
	_, st := testServer(t)

	// Populate database with request details for conn1, conn2, conn3
	for i := 0; i < 50; i++ {
		connID := fmt.Sprintf("conn%d", (i%3)+1)

		err := st.InsertRequestDetail(store.RequestDetail{ //nolint:exhaustruct // test struct
			ID:               fmt.Sprintf("req-%d", i),
			Timestamp:        fmt.Sprintf("2026-03-01T12:%02d:00Z", i),
			Provider:         "openai",
			Model:            "gpt-4o",
			ConnectionID:     connID,
			StatusCode:       200,
			DurationMs:       100,
			PromptTokens:     10,
			CompletionTokens: 20,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	server := &Server{st: st} //nolint:exhaustruct // test server

	out, prompt, completion, err := server.extractConnUsageData("conn1", 100)
	if err != nil {
		t.Fatalf("extractConnUsageData returned error: %v", err)
	}

	// conn1 should have 17 requests (i % 3 == 0: 0, 3, 6, ..., 48 -> 17 items)
	if len(out) != 17 {
		t.Fatalf("expected 17 items for conn1, got %d", len(out))
	}

	if prompt != 17*10 {
		t.Fatalf("expected prompt tokens %d, got %d", 17*10, prompt)
	}

	if completion != 17*20 {
		t.Fatalf("expected completion tokens %d, got %d", 17*20, completion)
	}
}

func TestHandleUsageByConnection(t *testing.T) {
	h, st := testServer(t)

	connID, err := st.CreateConnection("openai", "api_key", "TestConn", "sk-123", "")
	if err != nil {
		t.Fatal(err)
	}

	err = st.InsertRequestDetail(store.RequestDetail{ //nolint:exhaustruct // test struct
		ID:               "req-1",
		Timestamp:        "2026-03-01T12:00:00Z",
		Provider:         "openai",
		Model:            "gpt-4o",
		ConnectionID:     connID,
		StatusCode:       200,
		DurationMs:       120,
		PromptTokens:     15,
		CompletionTokens: 25,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usage/"+connID, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
	}
}

func BenchmarkExtractConnUsageData(b *testing.B) {
	dir := b.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = st.Close()
	}()

	// Seed 1000 request details across 10 connections
	for i := 0; i < 1000; i++ {
		connID := fmt.Sprintf("conn-%d", i%10)

		if insErr := st.InsertRequestDetail(store.RequestDetail{ //nolint:exhaustruct // test struct
			ID:               fmt.Sprintf("req-%d", i),
			Timestamp:        fmt.Sprintf("2026-03-01T%02d:%02d:%02dZ", (i/3600)%24, (i/60)%60, i%60),
			Provider:         "openai",
			Model:            "gpt-4o",
			ConnectionID:     connID,
			StatusCode:       200,
			DurationMs:       100,
			PromptTokens:     100,
			CompletionTokens: 50,
		}); insErr != nil {
			b.Fatal(insErr)
		}
	}

	server := &Server{st: st} //nolint:exhaustruct // test server

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		targetConn := fmt.Sprintf("conn-%d", i%10)

		_, _, _, err := server.extractConnUsageData(targetConn, 100)
		if err != nil {
			b.Fatal(err)
		}
	}
}
