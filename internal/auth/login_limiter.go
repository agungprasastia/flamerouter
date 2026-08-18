package auth

import (
	"flamerouter/internal/config"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxFailsBeforeLock = 5
	failWindow         = time.Hour
)

var lockSteps = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	10 * time.Minute,
	30 * time.Minute,
}

type loginEntry struct {
	lockUntil  time.Time
	lastFailAt time.Time
	fails      int
	lockLevel  int
}

var (
	loginMu       sync.Mutex
	loginAttempts = map[string]*loginEntry{}
	loginNow      = time.Now // test override
)

// ResetLoginLimiterForTest clears all attempt state.
func ResetLoginLimiterForTest() {
	loginMu.Lock()
	defer loginMu.Unlock()

	loginAttempts = map[string]*loginEntry{}
}

func clearLockUntilForTest(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()

	if e := loginAttempts[ip]; e != nil {
		e.lockUntil = time.Time{}
	}
}

func getEntry(ip string) *loginEntry {
	e := loginAttempts[ip]
	if e == nil {
		return nil
	}

	now := loginNow()
	// Auto reset if window expired and not currently locked
	if !e.lastFailAt.IsZero() && now.Sub(e.lastFailAt) > failWindow &&
		(e.lockUntil.IsZero() || !now.Before(e.lockUntil)) {
		delete(loginAttempts, ip)
		return nil
	}

	return e
}

// CheckLock reports whether ip is locked and seconds until unlock.
func CheckLock(ip string) (locked bool, retryAfterSec int) {
	loginMu.Lock()
	defer loginMu.Unlock()

	e := getEntry(ip)
	if e == nil || e.lockUntil.IsZero() {
		return false, 0
	}

	remaining := e.lockUntil.Sub(loginNow())
	if remaining <= 0 {
		return false, 0
	}

	sec := int(remaining.Seconds())
	if remaining%time.Second != 0 {
		sec++ // ceil
	}

	return true, sec
}

// RecordFail records a failed login. Returns remaining attempts before next lock.
func RecordFail(ip string) (remainingBeforeLock int) {
	loginMu.Lock()
	defer loginMu.Unlock()

	e := getEntry(ip)
	if e == nil {
		e = &loginEntry{
			lockUntil:  time.Time{},
			lastFailAt: time.Time{},
			fails:      0,
			lockLevel:  0,
		}
	}

	e.fails++

	e.lastFailAt = loginNow()
	if e.fails >= maxFailsBeforeLock {
		idx := e.lockLevel
		if idx >= len(lockSteps) {
			idx = len(lockSteps) - 1
		}

		e.lockUntil = loginNow().Add(lockSteps[idx])
		e.lockLevel++
		e.fails = 0
	}

	loginAttempts[ip] = e

	rem := maxFailsBeforeLock - e.fails
	if rem < 0 {
		rem = 0
	}

	return rem
}

// RecordSuccess clears all lock state for ip.
func RecordSuccess(ip string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginAttempts, ip)
}

// ClientIP extracts client IP address safely, checking proxy headers only if trusted.
func ClientIP(r *http.Request) string {
	if config.TrustProxy() {
		if realIP := r.Header.Get("x-9r-real-ip"); realIP != "" {
			return strings.TrimSpace(realIP)
		}

		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
	}

	if r.RemoteAddr != "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil && host != "" {
			return host
		}

		return r.RemoteAddr
	}

	return "unknown"
}
