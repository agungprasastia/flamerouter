package mitm

import (
	"log"
	"sync"
	"time"
)

const (
	maxRestarts    = 5
	restartResetMs = 5 * time.Minute
)

var restartDelays = []time.Duration{
	5 * time.Second,
	10 * time.Second,
	20 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

// Restarter auto-restarts MITM server up to maxRestarts with backoff.
type Restarter struct {
	lastStart  time.Time
	startFn    func(addr string) error
	addr       string
	count      int
	mu         sync.Mutex
	restarting bool
	enabled    bool
}

func NewRestarter(startFn func(addr string) error) *Restarter {
	return &Restarter{startFn: startFn, enabled: true}
}

func (r *Restarter) SetEnabled(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = v
}

func (r *Restarter) MarkStarted(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastStart = time.Now()
	r.addr = addr
	r.restarting = false
}

func (r *Restarter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count = 0
	r.restarting = false
}

// ScheduleRestart runs backoff restart in a goroutine (non-blocking).
func (r *Restarter) ScheduleRestart() {
	r.mu.Lock()
	if r.restarting || !r.enabled {
		r.mu.Unlock()
		return
	}

	r.restarting = true
	if time.Since(r.lastStart) >= restartResetMs {
		r.count = 0
	}

	if r.count >= maxRestarts {
		log.Printf("[mitm] max restart attempts reached (%d)", maxRestarts)

		r.restarting = false
		r.mu.Unlock()

		return
	}

	attempt := r.count
	delay := restartDelays[min(attempt, len(restartDelays)-1)]
	r.count++
	addr := r.addr
	startFn := r.startFn
	n := r.count
	r.mu.Unlock()

	log.Printf("[mitm] restarting in %v (%d/%d)", delay, n, maxRestarts)

	go func() {
		time.Sleep(delay)
		r.mu.Lock()
		if !r.enabled {
			r.restarting = false
			r.mu.Unlock()

			return
		}
		r.mu.Unlock()

		if startFn == nil || addr == "" {
			r.mu.Lock()
			r.restarting = false
			r.mu.Unlock()

			return
		}

		if err := startFn(addr); err != nil {
			log.Printf("[mitm] restart attempt %d failed: %v", n, err)
			r.mu.Lock()
			r.restarting = false
			r.mu.Unlock()
			r.ScheduleRestart()

			return
		}

		r.mu.Lock()
		r.count = 0
		r.lastStart = time.Now()
		r.restarting = false
		r.mu.Unlock()
		log.Printf("[mitm] restarted successfully")
	}()
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
