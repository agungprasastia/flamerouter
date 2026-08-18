package config_test

import (
	"flamerouter/internal/opensse/config"
	"testing"
	"time"
)

func TestCheckFallbackError_429(t *testing.T) {
	fb, cd, lvl := config.CheckFallbackError(429, "", 0)
	if !fb {
		t.Fatal("expected fallback")
	}

	if cd != config.GetQuotaCooldown(1) {
		t.Fatalf("cooldown=%d expected %d", cd, config.GetQuotaCooldown(1))
	}

	if lvl != 1 {
		t.Fatalf("level=%d", lvl)
	}
}

func TestCheckFallbackError_ExponentialBackoff(t *testing.T) {
	_, _, lvl1 := config.CheckFallbackError(429, "", 0)
	_, cd2, lvl2 := config.CheckFallbackError(429, "", lvl1)

	if cd2 <= config.GetQuotaCooldown(lvl1) {
		t.Fatal("expected higher cooldown")
	}

	if lvl2 != 2 {
		t.Fatalf("level=%d", lvl2)
	}
}

func TestCheckFallbackError_TextMatch(t *testing.T) {
	fb, cd, _ := config.CheckFallbackError(200, "insufficient_quota exceeded", 0)
	if !fb || cd != config.GetQuotaCooldown(1) {
		t.Fatalf("fb=%v cd=%d", fb, cd)
	}
}

func TestCheckFallbackError_Transient(t *testing.T) {
	fb, cd, _ := config.CheckFallbackError(500, "random error", 0)
	if !fb || cd != config.TransientCooldownMs {
		t.Fatalf("fb=%v cd=%d", fb, cd)
	}
}

func TestCheckFallbackError_ClientErrorNonFallback(t *testing.T) {
	// 400 Bad Request without quota/auth text should not fallback or cooldown
	fb, cd, _ := config.CheckFallbackError(400, "invalid model parameter", 0)
	if fb || cd != 0 {
		t.Fatalf("expected no fallback for 400 client error, got fb=%v cd=%d", fb, cd)
	}

	// 404 Model Not Found without quota/auth text should not fallback or cooldown
	fb, cd, _ = config.CheckFallbackError(404, "model not found", 0)
	if fb || cd != 0 {
		t.Fatalf("expected no fallback for 404 client error, got fb=%v cd=%d", fb, cd)
	}
}

func TestGetUnavailableUntil(t *testing.T) {
	until := config.GetUnavailableUntil(5000)

	parsed, err := time.Parse(time.RFC3339, until)
	if err != nil {
		t.Fatal(err)
	}

	if time.Until(parsed) > 6*time.Second || time.Until(parsed) < 4*time.Second {
		t.Fatalf("unexpected time: %v", time.Until(parsed))
	}
}

func TestParseResetDelayFromError(t *testing.T) {
	// Antigravity text
	text := "Your quota will reset after 2h7m23s"
	expected := int64((2*3600 + 7*60 + 23) * 1000)
	delay := config.ParseResetDelayFromError(nil, text)

	if delay != expected {
		t.Fatalf("expected %d, got %d", expected, delay)
	}

	// Codex JSON error with resets_in_seconds
	jsonErr := `{"error":{"type":"usage_limit_reached","resets_in_seconds":120}}`
	delayJSON := config.ParseResetDelayFromError(nil, jsonErr)

	if delayJSON != 120000 {
		t.Fatalf("expected 120000, got %d", delayJSON)
	}
}
