package ops

import "testing"

func TestConsoleLogRing(t *testing.T) {
	c := NewConsoleLog(3)
	c.Append("a")
	c.Append("b")
	c.Append("c")
	c.Append("d")
	got := c.Get()
	if len(got) != 3 || got[0] != "b" || got[2] != "d" {
		t.Fatalf("ring: %v", got)
	}
	c.Clear()
	if len(c.Get()) != 0 {
		t.Fatal("clear")
	}
}
