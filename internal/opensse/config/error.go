// Package config provides configuration models and error fallback definitions.
package config

import (
	"strings"
	"time"
)

// ErrorRule defines matching criteria and fallback/cooldown behavior for error responses.
type ErrorRule struct {
	Text       string
	Status     int
	CooldownMs int64
	Backoff    bool
}

// BackoffConfig holds exponential backoff parameters for quota/rate limit errors.
var BackoffConfig = struct {
	Base     int64
	Max      int64
	MaxLevel int
}{
	Base:     2000,
	Max:      5 * 60 * 1000,
	MaxLevel: 15,
}

// TransientCooldownMs is the default cooldown duration for transient errors.
const TransientCooldownMs = 30 * 1000

// ErrorRules lists known error patterns and their backoff/cooldown configurations.
var ErrorRules = []ErrorRule{
	{Text: "insufficient_quota", Status: 0, CooldownMs: 0, Backoff: true},
	{Text: "quota_exceeded", Status: 0, CooldownMs: 0, Backoff: true},
	{Text: "rate_limit", Status: 0, CooldownMs: 0, Backoff: true},
	{Text: "too many requests", Status: 0, CooldownMs: 0, Backoff: true},
	{Text: "requests per minute", Status: 0, CooldownMs: 0, Backoff: true},
	{Text: "please slow down", Status: 0, CooldownMs: 0, Backoff: true},
	{Text: "you have exceeded", Status: 0, CooldownMs: 0, Backoff: true},
	{Text: "current quota", Status: 0, CooldownMs: 0, Backoff: true},
	{Text: "", Status: 429, CooldownMs: 0, Backoff: true},
	{Text: "", Status: 402, CooldownMs: 5 * 60 * 1000, Backoff: false},
	{Text: "invalid_api_key", Status: 0, CooldownMs: 2 * 60 * 1000, Backoff: false},
	{Text: "authentication_error", Status: 0, CooldownMs: 2 * 60 * 1000, Backoff: false},
	{Text: "", Status: 401, CooldownMs: 2 * 60 * 1000, Backoff: false},
	{Text: "", Status: 403, CooldownMs: 2 * 60 * 1000, Backoff: false},
}

// GetQuotaCooldown computes the backoff cooldown duration for a given level.
func GetQuotaCooldown(backoffLevel int) int64 {
	level := backoffLevel - 1
	if level < 0 {
		level = 0
	}

	if level > 30 {
		level = 30
	}

	shift := uint(level) // #nosec G115 -- level bounded between 0 and 30

	cooldown := BackoffConfig.Base * (1 << shift)
	if cooldown > BackoffConfig.Max {
		cooldown = BackoffConfig.Max
	}

	return cooldown
}

// CheckFallbackError checks whether a response status or error message matches fallback rules.
func CheckFallbackError(status int, errorText string, backoffLevel int) (shouldFallback bool, cooldownMs int64, newBackoffLevel int) {
	lower := strings.ToLower(errorText)

	for _, rule := range ErrorRules {
		if rule.Text != "" && strings.Contains(lower, rule.Text) {
			if rule.Backoff {
				newLevel := backoffLevel + 1
				if newLevel > BackoffConfig.MaxLevel {
					newLevel = BackoffConfig.MaxLevel
				}

				return true, GetQuotaCooldown(newLevel), newLevel
			}

			return true, rule.CooldownMs, backoffLevel
		}

		if rule.Status != 0 && rule.Status == status {
			if rule.Backoff {
				newLevel := backoffLevel + 1
				if newLevel > BackoffConfig.MaxLevel {
					newLevel = BackoffConfig.MaxLevel
				}

				return true, GetQuotaCooldown(newLevel), newLevel
			}

			return true, rule.CooldownMs, backoffLevel
		}
	}

	return true, TransientCooldownMs, backoffLevel
}

// IsAccountUnavailable checks if the specified unavailable timestamp is still in the future.
func IsAccountUnavailable(until string) bool {
	if until == "" {
		return false
	}

	t, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return false
	}

	return time.Now().Before(t)
}

// GetUnavailableUntil formats a future timestamp based on cooldown in milliseconds.
func GetUnavailableUntil(cooldownMs int64) string {
	return time.Now().Add(time.Duration(cooldownMs) * time.Millisecond).UTC().Format(time.RFC3339)
}
