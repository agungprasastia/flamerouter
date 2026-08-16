package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
)

const codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

func init() {
	RegisterQuotaHandler("codex", fetchCodexUsage)
}

func fetchCodexUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{Plan: "unknown", Message: "No Codex access token available."}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("Accept", "application/json")

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Failed to fetch Codex usage: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{Message: fmt.Sprintf("Codex connected. Usage API temporarily unavailable (%d).", res.StatusCode)}, nil
	}

	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{Message: "Codex connected. Invalid response JSON."}, nil
	}

	normalRL := extractCodexRateLimit(data)
	reviewRL := extractCodexReviewRateLimit(data)

	quotas := make(map[string]QuotaItem)
	appendCodexQuotaWindows(quotas, "", normalRL)
	appendCodexQuotaWindows(quotas, "review", reviewRL)

	plan, _ := data["plan_type"].(string)
	if plan == "" {
		if summary, ok := data["summary"].(map[string]any); ok {
			plan, _ = summary["plan"].(string)
		}
	}
	if plan == "" {
		plan = "unknown"
	}

	var limReached *bool
	if reached, ok := normalRL["limit_reached"].(bool); ok {
		limReached = &reached
	}
	var revLimReached *bool
	if reached, ok := reviewRL["limit_reached"].(bool); ok {
		revLimReached = &reached
	}

	var resetCredits *ResetCreditInfo
	if rcc, ok := data["rate_limit_reset_credits"].(map[string]any); ok {
		avail := int(toFiniteFloat(rcc["available_count"], 0))
		resetCredits = &ResetCreditInfo{AvailableCount: avail}
	}

	return &QuotaResult{
		Provider:           "codex",
		Plan:               plan,
		Quotas:             quotas,
		LimitReached:       limReached,
		ReviewLimitReached: revLimReached,
		ResetCredits:       resetCredits,
	}, nil
}

func extractCodexRateLimit(data map[string]any) map[string]any {
	if rl, ok := data["rate_limit"].(map[string]any); ok {
		return rl
	}
	if rl, ok := data["rate_limits"].(map[string]any); ok {
		return rl
	}
	if byID, ok := data["rate_limits_by_limit_id"].(map[string]any); ok {
		if c, ok := byID["codex"].(map[string]any); ok {
			return c
		}
	}
	return map[string]any{}
}

func extractCodexReviewRateLimit(data map[string]any) map[string]any {
	if rl, ok := data["code_review_rate_limit"].(map[string]any); ok {
		return rl
	}
	if rl, ok := data["review_rate_limit"].(map[string]any); ok {
		return rl
	}
	if byID, ok := data["rate_limits_by_limit_id"].(map[string]any); ok {
		for _, k := range []string{"code_review", "codex_review", "review"} {
			if v, ok := byID[k].(map[string]any); ok {
				return v
			}
		}
	}
	if addl, ok := data["additional_rate_limits"].([]any); ok {
		for _, item := range addl {
			if m, ok := item.(map[string]any); ok {
				id := strings.ToLower(fmt.Sprintf("%v", m["limit_name"]))
				if strings.Contains(id, "review") {
					return m
				}
			}
		}
	}
	return map[string]any{}
}

func appendCodexQuotaWindows(quotas map[string]QuotaItem, prefix string, rl map[string]any) {
	if len(rl) == 0 {
		return
	}
	primary, _ := rl["primary_window"].(map[string]any)
	if primary == nil {
		primary, _ = rl["primary"].(map[string]any)
	}
	secondary, _ := rl["secondary_window"].(map[string]any)
	if secondary == nil {
		secondary, _ = rl["secondary"].(map[string]any)
	}

	if primary != nil {
		k := "session"
		if prefix != "" {
			k = prefix + "_session"
		}
		quotas[k] = formatCodexWindow(primary)
	}
	if secondary != nil {
		k := "weekly"
		if prefix != "" {
			k = prefix + "_weekly"
		}
		quotas[k] = formatCodexWindow(secondary)
	}
}

func formatCodexWindow(w map[string]any) QuotaItem {
	val := toFiniteFloat(w["used_percent"], math.NaN())
	if math.IsNaN(val) {
		val = toFiniteFloat(w["percent_used"], 0)
	}
	used := math.Max(0, math.Min(100, val))
	rem := math.Max(0, 100.0-used)
	res := parseResetTime(w["reset_at"])
	if res == nil {
		res = parseResetTime(w["resets_at"])
	}
	if res == nil {
		res = parseResetTime(w["resetAt"])
	}
	return QuotaItem{
		Used:                used,
		Total:               100,
		Remaining:           rem,
		RemainingPercentage: rem,
		ResetAt:             res,
		Unlimited:           false,
	}
}
