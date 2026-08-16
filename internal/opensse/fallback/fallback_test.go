package fallback_test

import (
	"flamerouter/internal/opensse/fallback"
	"flamerouter/internal/store"
	"testing"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { st.Close() })

	return st
}

func TestMarkUnavailable(t *testing.T) {
	st := setupTestStore(t)
	_, _ = st.CreateConnection("openai", "api_key", "test-key", "sk-test", "https://api.openai.com/v1")

	f := fallback.New(st)

	_, _ = f.MarkUnavailable("conn1", 429, "rate limit exceeded", 0)

	state := f.GetState("conn1")
	if state.BackoffLevel != 1 {
		t.Fatalf("expected backoff level 1, got %d", state.BackoffLevel)
	}

	if state.UnavailableUntil == "" {
		t.Fatal("expected unavailable until to be set")
	}
}

func TestMarkUnavailable_ExponentialBackoff(t *testing.T) {
	st := setupTestStore(t)
	f := fallback.New(st)

	_, _ = f.MarkUnavailable("conn1", 429, "rate limit", 0)
	level1 := f.GetState("conn1").BackoffLevel

	_, _ = f.MarkUnavailable("conn1", 429, "rate limit", 0)
	level2 := f.GetState("conn1").BackoffLevel

	if level2 <= level1 {
		t.Fatalf("expected higher backoff level, got %d <= %d", level2, level1)
	}
}

func TestMarkUnavailable_Transient(t *testing.T) {
	st := setupTestStore(t)
	f := fallback.New(st)

	_, _ = f.MarkUnavailable("conn1", 500, "internal error", 0)

	state := f.GetState("conn1")
	if state.BackoffLevel != 0 {
		t.Fatalf("expected backoff level 0 for transient, got %d", state.BackoffLevel)
	}
}

func TestClearError(t *testing.T) {
	st := setupTestStore(t)
	f := fallback.New(st)

	_, _ = f.MarkUnavailable("conn1", 429, "rate limit", 0)
	f.ClearError("conn1")

	state := f.GetState("conn1")
	if state.BackoffLevel != 0 {
		t.Fatalf("expected backoff level 0 after clear, got %d", state.BackoffLevel)
	}
}

func TestSelectAccount(t *testing.T) {
	st := setupTestStore(t)
	_, _ = st.CreateConnection("openai", "api_key", "key1", "sk-1", "https://api.openai.com/v1")
	_, _ = st.CreateConnection("openai", "api_key", "key2", "sk-2", "https://api.openai.com/v1")

	f := fallback.New(st)

	conn, err := f.SelectAccount("openai")
	if err != nil {
		t.Fatal(err)
	}

	if conn == nil {
		t.Fatal("expected a connection")
	}
}

func TestSelectAccount_ExcludesUnavailable(t *testing.T) {
	st := setupTestStore(t)
	id1, _ := st.CreateConnection("openai", "api_key", "key1", "sk-1", "https://api.openai.com/v1")
	_, _ = st.CreateConnection("openai", "api_key", "key2", "sk-2", "https://api.openai.com/v1")

	f := fallback.New(st)
	_, _ = f.MarkUnavailable(id1, 429, "rate limit", 60000)

	conn, err := f.SelectAccountExcluding("openai", map[string]bool{id1: true})
	if err != nil {
		t.Fatal(err)
	}

	if conn == nil {
		t.Fatal("expected a connection after excluding")
	}

	if conn.ID == id1 {
		t.Fatal("should not return excluded connection")
	}
}

func TestSelectAccountWithStrategy_FillFirst(t *testing.T) {
	st := setupTestStore(t)
	id1, _ := st.CreateConnection("openai", "api_key", "key1", "sk-1", "https://api.openai.com/v1")
	id2, _ := st.CreateConnection("openai", "api_key", "key2", "sk-2", "https://api.openai.com/v1")
	f := fallback.New(st)
	exclude := map[string]bool{}

	c1, err := f.SelectAccountWithStrategy("openai", "fill-first", 0, exclude)
	if err != nil {
		t.Fatal(err)
	}

	if c1 == nil {
		t.Fatal("expected connection")
	}
	// fill-first: same priority-order pick every time (first available)
	c2, err := f.SelectAccountWithStrategy("openai", "fill-first", 0, exclude)
	if err != nil {
		t.Fatal(err)
	}

	if c2.ID != c1.ID {
		t.Fatalf("fill-first should stick to first available, got %s then %s", c1.ID, c2.ID)
	}
	// empty strategy defaults to fill-first
	c3, err := f.SelectAccountWithStrategy("openai", "", 0, exclude)
	if err != nil {
		t.Fatal(err)
	}

	if c3.ID != c1.ID {
		t.Fatalf("default strategy should match fill-first")
	}

	_ = id1
	_ = id2
}

func TestSelectAccountWithStrategy_RoundRobin(t *testing.T) {
	st := setupTestStore(t)
	id1, _ := st.CreateConnection("openai", "api_key", "key1", "sk-1", "https://api.openai.com/v1")
	id2, _ := st.CreateConnection("openai", "api_key", "key2", "sk-2", "https://api.openai.com/v1")
	f := fallback.New(st)
	exclude := map[string]bool{}
	sticky := 2

	var ids []string

	for i := 0; i < 4; i++ {
		c, err := f.SelectAccountWithStrategy("openai", "round-robin", sticky, exclude)
		if err != nil {
			t.Fatal(err)
		}

		if c == nil {
			t.Fatal("expected connection")
		}

		ids = append(ids, c.ID)
	}
	// sticky=2: A,A,B,B (or B,B,A,A depending first pick)
	if ids[0] != ids[1] {
		t.Fatalf("sticky: first two should match, got %v", ids)
	}

	if ids[2] != ids[3] {
		t.Fatalf("sticky: last two should match, got %v", ids)
	}

	if ids[0] == ids[2] {
		t.Fatalf("should rotate after sticky limit, got %v", ids)
	}
	// both accounts used
	seen := map[string]bool{ids[0]: true, ids[2]: true}
	if !seen[id1] || !seen[id2] {
		t.Fatalf("expected both accounts, got %v (ids %s %s)", ids, id1, id2)
	}
}
