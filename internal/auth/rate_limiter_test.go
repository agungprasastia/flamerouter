package auth

import (
	"testing"
	"time"
)

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("should allow attempt %d", i+1)
		}

		rl.Record("1.2.3.4")
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		rl.Record("1.2.3.4")
	}

	if rl.Allow("1.2.3.4") {
		t.Fatal("should block after 3 attempts")
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	rl.Record("1.1.1.1")

	if rl.Allow("1.1.1.1") {
		t.Fatal("should block 1.1.1.1")
	}

	if !rl.Allow("2.2.2.2") {
		t.Fatal("should allow 2.2.2.2")
	}
}
