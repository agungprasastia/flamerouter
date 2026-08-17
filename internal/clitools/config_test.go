package clitools

import (
	"flamerouter/internal/store"
	"path/filepath"
	"testing"
)

func checkManagerStatus(t *testing.T, m *Manager) {
	t.Helper()

	all := m.AllStatuses()
	if all["claude"] == nil {
		t.Fatal("missing claude status")
	}

	if !Known("claude") || Known("nope") {
		t.Fatal("Known")
	}
}

func TestManagerGetPatchStatuses(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if clErr := st.Close(); clErr != nil {
			t.Errorf("store close error: %v", clErr)
		}
	})

	m := New(st)

	s, err := m.GetSettings("claude")
	if err != nil || len(s) != 0 {
		t.Fatalf("empty settings: %v %v", s, err)
	}

	patchErr := m.PatchSettings("claude", map[string]any{"enabled": true, "path": "/bin/claude"})
	if patchErr != nil {
		t.Fatal(patchErr)
	}

	s, err = m.GetSettings("claude")
	if err != nil || s["path"] != "/bin/claude" {
		t.Fatalf("patched: %v %v", s, err)
	}

	checkManagerStatus(t, m)
}
