package executor

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncodeDecodeCursorProtobufRoundtrip(t *testing.T) {
	body := []byte(`{"model":"gpt-4","messages":[]}`)

	enc := EncodeCursorProtobuf(body)
	if len(enc) == 0 {
		t.Fatal("encode empty")
	}

	if enc[0] != 0x0a {
		t.Fatalf("tag want 0x0a got %#x", enc[0])
	}

	dec := DecodeCursorProtobuf(enc)
	if !bytes.Equal(dec, body) {
		t.Fatalf("roundtrip: got %q want %q", dec, body)
	}
}

func TestDecodeCursorProtobufEmpty(t *testing.T) {
	if got := DecodeCursorProtobuf(nil); len(got) != 0 {
		t.Fatalf("nil: %q", got)
	}

	if got := DecodeCursorProtobuf([]byte{0x0a}); len(got) != 0 {
		t.Fatalf("tag only: %q", got)
	}
}

func TestGenerateCursorChecksum(t *testing.T) {
	machineID := "machine-abc"
	cs := GenerateCursorChecksum(machineID)

	if cs == "" {
		t.Fatal("empty checksum")
	}

	if !strings.HasSuffix(cs, machineID) {
		t.Fatalf("checksum %q should end with machineID %q", cs, machineID)
	}

	// Jyh cipher 6 bytes -> base64 unpadded = 8 chars + len(machineID)
	if len(cs) != 8+len(machineID) {
		t.Fatalf("len want %d got %d (%q)", 8+len(machineID), len(cs), cs)
	}
}

func TestKiroSessionManager(t *testing.T) {
	m := NewKiroSessionManager()
	if m.Get("c1") != "" {
		t.Fatal("empty get")
	}

	m.Set("c1", "sess-1")

	if m.Get("c1") != "sess-1" {
		t.Fatalf("get: %q", m.Get("c1"))
	}

	m.Set("c1", "sess-2")

	if m.Get("c1") != "sess-2" {
		t.Fatalf("overwrite: %q", m.Get("c1"))
	}
}
