package store_test

import (
	"flamerouter/internal/store"
	"fmt"
	"strings"
	"testing"
)

func BenchmarkQueryRequestDetailsSummary(b *testing.B) {
	dir := b.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()

	body := strings.Repeat("a", 5000)
	resp := strings.Repeat("b", 5000)

	for i := 0; i < 1000; i++ {
		_ = st.InsertRequestDetail(store.RequestDetail{
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
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := st.QueryRequestDetailsSummary(100)
		if err != nil {
			b.Fatal(err)
		}
	}
}
