// Package auth provides authentication, session management, and rate limiting.
package auth

import (
	"sync"
	"time"
)

// RateLimiter tracks login attempts per IP with sliding window.
type RateLimiter struct {
	attempts    map[string][]time.Time
	maxAttempts int
	window      time.Duration
	mu          sync.Mutex
}

// NewRateLimiter creates a new RateLimiter instance with given attempts and window.
func NewRateLimiter(maxAttempts int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		attempts:    make(map[string][]time.Time),
		maxAttempts: maxAttempts,
		window:      window,
		mu:          sync.Mutex{},
	}
}

// Allow checks if the IP can attempt a login.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	var recent []time.Time

	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	rl.attempts[ip] = recent

	return len(recent) < rl.maxAttempts
}

// Record adds a login attempt for the IP.
func (rl *RateLimiter) Record(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.attempts[ip] = append(rl.attempts[ip], time.Now())
}
