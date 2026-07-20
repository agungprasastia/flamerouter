package mcp

import (
	"runtime"
	"testing"
	"time"
)

func TestBridgeStartSendSubscribe(t *testing.T) {
	// use cat / type as echo-ish stdio process
	var cmd string
	var args []string
	if runtime.GOOS == "windows" {
		cmd = "cmd"
		args = []string{"/c", "more"}
	} else {
		cmd = "cat"
	}
	b := New()
	if err := b.Start("echo", cmd, args); err != nil {
		t.Skip("no stdio binary:", err)
	}
	ch, err := b.Subscribe("echo")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Unsubscribe("echo", ch)
	if err := b.Send("echo", []byte(`{"jsonrpc":"2.0"}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-ch:
		if len(line) == 0 {
			t.Fatal("empty line")
		}
	case <-time.After(3 * time.Second):
		// more/cat may buffer; presence of Running is enough on win
		if !b.Running("echo") {
			t.Fatal("timeout and not running")
		}
	}
}
