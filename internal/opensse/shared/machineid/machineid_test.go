package machineid

import (
	"testing"
)

func TestGetConsistentMachineID(t *testing.T) {
	id1 := GetConsistentMachineID("test-salt")
	if len(id1) != 16 {
		t.Fatalf("expected length 16, got %d (%q)", len(id1), id1)
	}

	// Consistency test with same salt
	id2 := GetConsistentMachineID("test-salt")
	if id1 != id2 {
		t.Fatalf("expected consistent ID for same salt, got %q != %q", id1, id2)
	}

	// Different salt should produce different ID
	id3 := GetConsistentMachineID("another-salt")
	if len(id3) != 16 {
		t.Fatalf("expected length 16, got %d (%q)", len(id3), id3)
	}

	if id1 == id3 {
		t.Fatalf("expected different IDs for different salts, got %q == %q", id1, id3)
	}

	// Empty salt
	idEmpty := GetConsistentMachineID("")
	if len(idEmpty) != 16 {
		t.Fatalf("expected length 16 for empty salt, got %d (%q)", len(idEmpty), idEmpty)
	}
}

func TestGetRawMachineID(t *testing.T) {
	raw := getRawMachineID()
	if raw == "" {
		t.Fatal("expected non-empty raw machine ID")
	}

	raw2 := getRawMachineID()
	if raw != raw2 {
		t.Fatalf("expected raw machine ID to be cached and identical, got %q != %q", raw, raw2)
	}
}
