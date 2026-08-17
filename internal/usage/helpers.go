package usage

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

func parseResetTimeString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}

	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return formatUnixTimestamp(n)
	}

	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			iso := parsed.UTC().Format(time.RFC3339Nano)

			return &iso
		}
	}

	return nil
}

func parseResetTimeNumeric(v any) *string {
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) || t <= 0 {
			return nil
		}

		return formatUnixTimestamp(int64(t))
	case int64:
		if t <= 0 {
			return nil
		}

		return formatUnixTimestamp(t)
	case int:
		if t <= 0 {
			return nil
		}

		return formatUnixTimestamp(int64(t))
	default:
		return parseResetTimeJSONNumber(v)
	}
}

func parseResetTimeJSONNumber(v any) *string {
	if num, ok := v.(json.Number); ok {
		if n, err := num.Int64(); err == nil {
			return formatUnixTimestamp(n)
		}

		if f, err := num.Float64(); err == nil {
			return formatUnixTimestamp(int64(f))
		}
	}

	return nil
}

func parseResetTime(v any) *string {
	if v == nil {
		return nil
	}

	if s, ok := v.(string); ok {
		return parseResetTimeString(s)
	}

	if t, ok := v.(time.Time); ok {
		iso := t.UTC().Format(time.RFC3339Nano)

		return &iso
	}

	return parseResetTimeNumeric(v)
}

func formatUnixTimestamp(n int64) *string {
	var t time.Time
	if n < 1000000000000 {
		t = time.Unix(n, 0).UTC()
	} else {
		t = time.UnixMilli(n).UTC()
	}

	iso := t.Format(time.RFC3339Nano)

	return &iso
}

func toFiniteFloatNumeric(v any, fallback float64) float64 {
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return fallback
		}

		return t
	case float32:
		f := float64(t)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fallback
		}

		return f
	default:
		return toFiniteFloatInt(v, fallback)
	}
}

func toFiniteFloatInt(v any, fallback float64) float64 {
	switch t := v.(type) {
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f
		}
	}

	return fallback
}

func toFiniteFloat(v any, fallback float64) float64 {
	if v == nil {
		return fallback
	}

	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return fallback
		}

		if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}

		return fallback
	}

	if m, ok := v.(map[string]any); ok {
		if val, exists := m["val"]; exists {
			return toFiniteFloat(val, fallback)
		}

		return fallback
	}

	return toFiniteFloatNumeric(v, fallback)
}

func makeQuota(used, total float64, resetAt *string, unlimited bool) QuotaItem {
	safeTotal := math.Max(0, total)
	safeUsed := math.Max(0, used)

	if unlimited || safeTotal == 0 {
		remPct := 0.0
		if unlimited {
			remPct = 100.0
		}

		return QuotaItem{
			ResetAt:             resetAt,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                safeUsed,
			Total:               0,
			Remaining:           0,
			RemainingPercentage: remPct,
			Unlimited:           true,
		}
	}

	rem := math.Max(0, safeTotal-safeUsed)
	pct := (rem / safeTotal) * 100.0

	return QuotaItem{
		ResetAt:             resetAt,
		Recurring:           nil,
		DisplayName:         "",
		Unit:                "",
		Used:                safeUsed,
		Total:               safeTotal,
		Remaining:           rem,
		RemainingPercentage: pct,
		Unlimited:           false,
	}
}
