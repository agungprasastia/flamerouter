package executor

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"strings"
	"testing"
)

func buildEventFrame(eventType string, payload any) []byte {
	// headers: :event-type (string type 7)
	name := []byte(":event-type")
	val := []byte(eventType)

	var hdr []byte
	hdr = append(hdr, byte(len(name)))
	hdr = append(hdr, name...)
	hdr = append(hdr, 7) // string type
	hdr = append(hdr, byte(len(val)>>8), byte(len(val)))
	hdr = append(hdr, val...)

	pl, err := json.Marshal(payload)
	if err != nil {
		pl = []byte("{}")
	}

	headersLen := len(hdr)
	totalLen := 12 + headersLen + len(pl) + 4
	out := make([]byte, totalLen)

	if totalLen <= math.MaxInt32 {
		binary.BigEndian.PutUint32(out[0:4], uint32(totalLen)) // #nosec G115
	}

	if headersLen <= math.MaxInt32 {
		binary.BigEndian.PutUint32(out[4:8], uint32(headersLen)) // #nosec G115
	}

	copy(out[12:], hdr)
	copy(out[12+headersLen:], pl)

	return out
}

func TestParseEventFrame(t *testing.T) {
	frame := buildEventFrame("assistantResponseEvent", map[string]any{"content": "hello"})

	headers, payload, ok := parseEventFrame(frame)
	if !ok {
		t.Fatal("parse failed")
	}

	if headers[":event-type"] != "assistantResponseEvent" {
		t.Fatalf("event type: %q", headers[":event-type"])
	}

	if payload["content"] != "hello" {
		t.Fatalf("payload: %#v", payload)
	}
}

func TestTransformKiroEventStream(t *testing.T) {
	frame1 := buildEventFrame("assistantResponseEvent", map[string]any{"content": "hi "})
	frame2 := buildEventFrame("assistantResponseEvent", map[string]any{"content": "there"})
	frame3 := buildEventFrame("messageStopEvent", map[string]any{})
	data := append(append(frame1, frame2...), frame3...)

	rc := transformKiroEventStream(strings.NewReader(string(data)), "claude-sonnet-4")
	defer func() {
		if err := rc.Close(); err != nil {
			_ = err
		}
	}()

	out, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}

	s := string(out)
	if !strings.Contains(s, `"content":"hi "`) && !strings.Contains(s, `"content": "hi "`) {
		// json marshal may not space
		if !strings.Contains(s, "hi ") {
			t.Fatalf("missing content: %s", s)
		}
	}

	if !strings.Contains(s, "[DONE]") {
		t.Fatalf("missing DONE: %s", s)
	}

	if !strings.Contains(s, "chat.completion.chunk") {
		t.Fatalf("missing chunk type: %s", s)
	}
}
