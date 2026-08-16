package usage

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

func parseResetTime(v any) *string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return formatUnixTimestamp(n)
		}
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			iso := parsed.UTC().Format(time.RFC3339Nano)
			return &iso
		}
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			iso := parsed.UTC().Format(time.RFC3339Nano)
			return &iso
		}
		if parsed, err := time.Parse("2006-01-02T15:04:05Z07:00", s); err == nil {
			iso := parsed.UTC().Format(time.RFC3339Nano)
			return &iso
		}
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
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return formatUnixTimestamp(n)
		}
		if f, err := t.Float64(); err == nil {
			return formatUnixTimestamp(int64(f))
		}
	case time.Time:
		iso := t.UTC().Format(time.RFC3339Nano)
		return &iso
	}
	return nil
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

func toFiniteFloat(v any, fallback float64) float64 {
	if v == nil {
		return fallback
	}
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
		return fallback
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return fallback
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	case map[string]any:
		if val, ok := t["val"]; ok {
			return toFiniteFloat(val, fallback)
		}
	}
	return fallback
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
			Used:                safeUsed,
			Total:               0,
			RemainingPercentage: remPct,
			ResetAt:             resetAt,
			Unlimited:           true,
		}
	}
	rem := math.Max(0, safeTotal-safeUsed)
	pct := (rem / safeTotal) * 100.0
	return QuotaItem{
		Used:                safeUsed,
		Total:               safeTotal,
		Remaining:           rem,
		RemainingPercentage: pct,
		ResetAt:             resetAt,
		Unlimited:           false,
	}
}
