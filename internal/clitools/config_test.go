package clitools

import (
	"flamerouter/internal/store"
	"path/filepath"
	"testing"
)

func TestManagerGetPatchStatuses(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	m := New(st)

	s, err := m.GetSettings("claude")
	if err != nil || len(s) != 0 {
		t.Fatalf("empty settings: %v %v", s, err)
	}

	if err := m.PatchSettings("claude", map[string]any{"enabled": true, "path": "/bin/claude"}); err != nil {
		t.Fatal(err)
	}

	s, err = m.GetSettings("claude")
	if err != nil || s["path"] != "/bin/claude" {
		t.Fatalf("patched: %v %v", s, err)
	}

	all := m.AllStatuses()
	if all["claude"] == nil {
		t.Fatal("missing claude status")
	}

	if !Known("claude") || Known("nope") {
		t.Fatal("Known")
	}
}
