package fallback_test

import (
	"path/filepath"
	"testing"

	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestRetryClassification(t *testing.T) {
	st := newTestStore(t)
	id, err := st.CreateConnection("openai", "api_key", "c1", "sk-1", "")
	if err != nil {
		t.Fatal(err)
	}

	fb := fallback.New(st)

	// Rate limit status 429
	should, cd := fb.MarkUnavailable(id, 429, "too many requests", 0)
	if !should || cd <= 0 {
		t.Fatalf("429 should fallback, cd=%d", cd)
	}
	state := fb.GetState(id)
	if state.BackoffLevel != 1 {
		t.Fatalf("backoff level = %d, want 1", state.BackoffLevel)
	}

	// Exponential backoff increase on second rate limit
	_, cd2 := fb.MarkUnavailable(id, 429, "rate_limit", 0)
	if cd2 <= cd {
		t.Fatalf("exponential backoff expected cd2 (%d) > cd (%d)", cd2, cd)
	}

	// 401 Auth error
	fb.ClearError(id)
	should, cdAuth := fb.MarkUnavailable(id, 401, "unauthorized", 0)
	if !should || cdAuth != 120000 {
		t.Fatalf("401 cooldown want 120000, got %d", cdAuth)
	}

	// 500 Transient server error
	fb.ClearError(id)
	should, cd500 := fb.MarkUnavailable(id, 500, "internal server error", 0)
	if !should || cd500 != 30000 {
		t.Fatalf("500 cooldown want 30000, got %d", cd500)
	}
}

func TestSelectAccountExcludingRotates(t *testing.T) {
	st := newTestStore(t)
	id1, _ := st.CreateConnection("openai", "api_key", "c1", "sk-1", "")
	id2, _ := st.CreateConnection("openai", "api_key", "c2", "sk-2", "")

	fb := fallback.New(st)

	// Exclude id1 -> must return id2
	conn, err := fb.SelectAccountExcluding("openai", map[string]bool{id1: true})
	if err != nil {
		t.Fatal(err)
	}
	if conn == nil || conn.ID != id2 {
		t.Fatalf("expected id2, got %v", conn)
	}

	// Exclude both -> must return nil
	conn, err = fb.SelectAccountExcluding("openai", map[string]bool{id1: true, id2: true})
	if err != nil {
		t.Fatal(err)
	}
	if conn != nil {
		t.Fatalf("expected nil when all excluded, got %v", conn)
	}
}
