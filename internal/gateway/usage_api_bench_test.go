package gateway

import (
	"flamerouter/internal/auth"
	"flamerouter/internal/config"
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func BenchmarkHandleUsageStats(b *testing.B) {
	dir := b.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		b.Fatal(err)
	}

	defer func() {
		_ = st.Close()
	}()

	cfg := &config.Config{ //nolint:exhaustruct
		DataDir:       dir,
		JWTSecret:     "test-secret-long-enough",
		APIKeySecret:  "test-api-key-secret",
		MachineIDSalt: "test-salt",
	}
	keys := auth.New(cfg.APIKeySecret)
	s := &Server{ //nolint:exhaustruct
		cfg:     cfg,
		st:      st,
		keys:    keys,
		fb:      fallback.New(st),
		jwt:     auth.NewJWTManager(cfg.JWTSecret),
		session: auth.NewSessionHandler(auth.NewJWTManager(cfg.JWTSecret), st, "123456"),
		mux:     http.NewServeMux(),
	}
	s.routes()

	body := strings.Repeat("a", 5000)
	resp := strings.Repeat("b", 5000)

	for i := 0; i < 1000; i++ {
		insErr := st.InsertRequestDetail(store.RequestDetail{
			ID:               fmt.Sprintf("id-%d", i),
			Timestamp:        fmt.Sprintf("2026-07-20T10:%02d:%02d.000Z", (i/60)%60, i%60),
			Provider:         "openai",
			Model:            "gpt-4o",
			ConnectionID:     "conn-1",
			StatusCode:       200,
			DurationMs:       120,
			PromptTokens:     100,
			CompletionTokens: 50,
			CachedTokens:     10,
			Cost:             0.002,
			RequestBody:      body,
			ResponsePreview:  resp,
		})
		if insErr != nil {
			b.Fatal(insErr)
		}
	}

	if insDailyErr := st.InsertUsageDaily("2026-07-20", "openai", "gpt-4o", 1000, 100000, 50000, 10000, 2.0); insDailyErr != nil {
		b.Fatal(insDailyErr)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/usage/stats?from=2026-07-01&to=2026-07-31", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			b.Fatalf("expected status 200, got %d", rr.Code)
		}
	}
}
