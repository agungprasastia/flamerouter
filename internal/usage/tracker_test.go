package usage

import (
	"path/filepath"
	"testing"
	"time"

	"flamerouter/internal/store"
)

func TestTracker_PersistsUsage(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	hub := NewStreamHub()
	tr := NewTracker(st, hub)
	tr.Track(Record{
		Provider: "openai", Model: "gpt-4o", ConnectionID: "c1",
		PromptTokens: 10, CompletionTokens: 5, StatusCode: 200,
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
