// Package mitm provides local HTTPS MITM proxy and certificate generation for developer tools.
package mitm

import (
	"log"
	"sync"
	"time"
)

const (
	maxRestarts  = 5
	restartReset = 5 * time.Minute
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

// NewRestarter initializes a new MITM restarter.
func NewRestarter(startFn func(addr string) error) *Restarter {
	return &Restarter{
		lastStart:  time.Time{},
		startFn:    startFn,
		addr:       "",
		count:      0,
		mu:         sync.Mutex{},
		restarting: false,
		enabled:    true,
	}
}

// SetEnabled enables or disables auto-restarts.
func (r *Restarter) SetEnabled(v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = v
}

// MarkStarted records that the server started successfully on addr.
func (r *Restarter) MarkStarted(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastStart = time.Now()
	r.addr = addr
	r.restarting = false
}

// Reset clears the restart counter.
func (r *Restarter) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count = 0
	r.restarting = false
}

// ScheduleRestart runs backoff restart in a goroutine (non-blocking).
func (r *Restarter) ScheduleRestart() {
	delay, n, addr, startFn, ok := r.prepareRestart()
	if !ok {
		return
	}

	log.Printf("[mitm] restarting in %v (%d/%d)", delay, n, maxRestarts)

	go r.executeRestart(delay, n, addr, startFn)
}

func (r *Restarter) prepareRestart() (time.Duration, int, string, func(string) error, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.restarting || !r.enabled {
		return 0, 0, "", nil, false
	}

	r.restarting = true
	if time.Since(r.lastStart) >= restartReset {
		r.count = 0
	}

	if r.count >= maxRestarts {
		log.Printf("[mitm] max restart attempts reached (%d)", maxRestarts)

		r.restarting = false

		return 0, 0, "", nil, false
	}

	attempt := r.count
	delay := restartDelays[min(attempt, len(restartDelays)-1)]
	r.count++

	return delay, r.count, r.addr, r.startFn, true
}

func (r *Restarter) executeRestart(delay time.Duration, n int, addr string, startFn func(string) error) {
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
}

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}
