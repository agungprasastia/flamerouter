package config

import (
	"strings"
	"time"
)

type ErrorRule struct {
	Text       string
	Status     int
	CooldownMs int64
	Backoff    bool
}

var BackoffConfig = struct {
	Base     int64
	Max      int64
	MaxLevel int
}{
	Base:     2000,
	Max:      5 * 60 * 1000,
	MaxLevel: 15,
}

const TransientCooldownMs = 30 * 1000

var ErrorRules = []ErrorRule{
	{Text: "insufficient_quota", Backoff: true},
	{Text: "quota_exceeded", Backoff: true},
	{Text: "rate_limit", Backoff: true},
	{Text: "too many requests", Backoff: true},
	{Text: "requests per minute", Backoff: true},
	{Text: "please slow down", Backoff: true},
	{Text: "you have exceeded", Backoff: true},
	{Text: "current quota", Backoff: true},
	{Status: 429, Backoff: true},
	{Status: 402, CooldownMs: 5 * 60 * 1000},
	{Text: "invalid_api_key", CooldownMs: 2 * 60 * 1000},
	{Text: "authentication_error", CooldownMs: 2 * 60 * 1000},
	{Status: 401, CooldownMs: 2 * 60 * 1000},
	{Status: 403, CooldownMs: 2 * 60 * 1000},
}

func GetQuotaCooldown(backoffLevel int) int64 {
	level := backoffLevel - 1
	if level < 0 {
		level = 0
	}

	cooldown := BackoffConfig.Base * (1 << uint(level))
	if cooldown > BackoffConfig.Max {
		cooldown = BackoffConfig.Max
	}

	return cooldown
}

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

func GetUnavailableUntil(cooldownMs int64) string {
	return time.Now().Add(time.Duration(cooldownMs) * time.Millisecond).UTC().Format(time.RFC3339)
}
