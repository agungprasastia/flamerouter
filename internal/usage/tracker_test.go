package usage

import (
	"flamerouter/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func TestTracker_PersistsUsage(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}()

	hub := NewStreamHub()
	tr := NewTracker(st, hub)
	tr.Track(Record{
		Provider:         "openai",
		Model:            "gpt-4o",
		ConnectionID:     "c1",
		PromptTokens:     10,
		CompletionTokens: 5,
		CachedTokens:     2,
		Cost:             0.0001,
		StatusCode:       200,
		RequestBody:      "",
		Client:           "",
		SourceFormat:     "",
		TargetFormat:     "",
		ErrorText:        "",
		ResponsePreview:  "",
		DurationMs:       0,
	})
	// allow async loop
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		from, to := DefaultRange()

		rows, err := st.QueryUsageDaily(from, to)
		if err != nil {
			t.Fatal(err)
		}

		if len(rows) > 0 {
			tr.Close()

			return
		}

		time.Sleep(20 * time.Millisecond)
	}

	tr.Close()
	t.Fatal("expected usage_daily row")
}
