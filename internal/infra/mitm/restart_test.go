package mitm

import (
	"testing"
	"time"
)

func TestRestarter_GivesUpAtMax(t *testing.T) {
	r := NewRestarter(func(_ string) error {
		return errRestart
	})
	r.addr = ":0"
	r.MarkStarted(":0")

	old := restartDelays
	restartDelays = []time.Duration{time.Millisecond}

	t.Cleanup(func() { restartDelays = old })

	// schedule once; failures re-schedule until max
	r.ScheduleRestart()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		c, restarting := r.count, r.restarting
		r.mu.Unlock()

		if c >= maxRestarts && !restarting {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	r.mu.Lock()
	c := r.count
	r.mu.Unlock()

	if c < maxRestarts {
		t.Fatalf("count=%d want >= %d", c, maxRestarts)
	}
}

func TestDefaultToolHandlers(t *testing.T) {
	h := DefaultToolHandlers("http://127.0.0.1:20128", "sk-test")
	if len(h) < 4 {
		t.Fatalf("handlers=%d", len(h))
	}

	if _, ok := h["api2.cursor.sh"]; !ok {
		t.Fatal("missing cursor host")
	}
}

var errRestart = &restartErr{}

type restartErr struct{}

func (e *restartErr) Error() string { return "fail" }
