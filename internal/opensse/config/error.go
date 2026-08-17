// Package config provides configuration models and error fallback definitions.
package config

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var antigravityResetRegex = regexp.MustCompile(`(?i)reset after (?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?`)

// ParseResetDelayFromError extracts retry delay in milliseconds from headers or error message.
// Handles Retry-After, x-ratelimit-reset-after, x-ratelimit-reset, OpenAI/Codex usage_limit_reached, and Antigravity reset strings.
func ParseResetDelayFromError(headers http.Header, errorText string) int64 {
	if headers != nil {
		if delay := parseHeadersResetDelay(headers); delay > 0 {
			return delay
		}
	}

	if errorText == "" {
		return 0
	}

	if delay := parseJSONResetsAt(errorText); delay > 0 {
		return delay
	}

	return parseAntigravityResetText(errorText)
}

func parseHeadersResetDelay(headers http.Header) int64 {
	if ra := headers.Get("Retry-After"); ra != "" {
		if delay := parseRetryAfterVal(ra); delay > 0 {
			return delay
		}
	}

	if rra := headers.Get("x-ratelimit-reset-after"); rra != "" {
		if sec, err := strconv.ParseInt(strings.TrimSpace(rra), 10, 64); err == nil && sec > 0 {
			return sec * 1000
		}
	}

	if rr := headers.Get("x-ratelimit-reset"); rr != "" {
		if sec, err := strconv.ParseInt(strings.TrimSpace(rr), 10, 64); err == nil && sec > 0 {
			diff := time.Until(time.Unix(sec, 0)).Milliseconds()
			if diff > 0 {
				return diff
			}
		}
	}

	return 0
}

func parseRetryAfterVal(ra string) int64 {
	if sec, err := strconv.ParseInt(strings.TrimSpace(ra), 10, 64); err == nil && sec > 0 {
		return sec * 1000
	}

	if t, err := http.ParseTime(ra); err == nil {
		diff := time.Until(t).Milliseconds()
		if diff > 0 {
			return diff
		}
	}

	return 0
}

func parseJSONResetsAt(text string) int64 {
	var body struct {
		Error struct {
			Type            string  `json:"type"`
			ResetsAt        float64 `json:"resets_at"`
			ResetsInSeconds float64 `json:"resets_in_seconds"`
		} `json:"error"`
	}

	if err := json.Unmarshal([]byte(text), &body); err != nil {
		return 0
	}

	if body.Error.ResetsAt > 0 {
		diff := time.Until(time.Unix(int64(body.Error.ResetsAt), 0)).Milliseconds()
		if diff > 0 {
			return diff
		}
	}

	if body.Error.ResetsInSeconds > 0 {
		return int64(body.Error.ResetsInSeconds * 1000)
	}

	return 0
}

func parseAntigravityResetText(text string) int64 {
	match := antigravityResetRegex.FindStringSubmatch(text)
	if match == nil {
		return 0
	}

	var totalMs int64

	if match[1] != "" {
		if h, err := strconv.ParseInt(match[1], 10, 64); err == nil {
			totalMs += h * 3600 * 1000
		}
	}

	if match[2] != "" {
		if m, err := strconv.ParseInt(match[2], 10, 64); err == nil {
			totalMs += m * 60 * 1000
		}
	}

	if match[3] != "" {
		if s, err := strconv.ParseInt(match[3], 10, 64); err == nil {
			totalMs += s * 1000
		}
	}

	return totalMs
}

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
