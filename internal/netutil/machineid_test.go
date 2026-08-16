package netutil

import (
	"testing"
)

func TestGetConsistentMachineID(t *testing.T) {
	id1 := GetConsistentMachineID("salt1")
	id2 := GetConsistentMachineID("salt1")

	if id1 == "" || len(id1) != 16 {
		t.Fatalf("unexpected id1: %q", id1)
	}

	if id1 != id2 {
		t.Fatalf("expected identical id, got %q != %q", id1, id2)
	}
}
