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

func parseCodexPlan(data map[string]any) string {
	if plan, ok := data["plan_type"].(string); ok && plan != "" {
		return plan
	}

	if summary, ok := data["summary"].(map[string]any); ok {
		if plan, ok := summary["plan"].(string); ok && plan != "" {
			return plan
		}
	}

	return "unknown"
}

func parseCodexData(data map[string]any) *QuotaResult {
	normalRL := extractCodexRateLimit(data)
	reviewRL := extractCodexReviewRateLimit(data)

	quotas := make(map[string]QuotaItem)
	appendCodexQuotaWindows(quotas, "", normalRL)
	appendCodexQuotaWindows(quotas, "review", reviewRL)

	plan := parseCodexPlan(data)

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
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      0,
		LimitReached:       limReached,
		ReviewLimitReached: revLimReached,
		IsQuotaExceeded:    nil,
		ResetCredits:       resetCredits,
		ResetsAt:           nil,
		Message:            "",
		Details:            nil,
		Quotas:             quotas,
	}
}

func fetchCodexUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{
			Provider:           "codex",
			Plan:               "unknown",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            "No Codex access token available.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, codexUsageURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Codex usage: %w", err)
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	return handleCodexResponse(res)
}

func handleCodexResponse(res *http.Response) (*QuotaResult, error) {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           "codex",
			Plan:               "",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            fmt.Sprintf("Codex connected. Usage API temporarily unavailable (%d).", res.StatusCode),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("codex connected. Invalid response JSON: %w", err)
	}

	return parseCodexData(data), nil
}

func extractCodexRateLimit(data map[string]any) map[string]any {
	if rl, ok := data["rate_limit"].(map[string]any); ok && rl != nil {
		return rl
	}

	if rl, ok := data["rate_limits"].(map[string]any); ok && rl != nil {
		return rl
	}

	if byID, ok := data["rate_limits_by_limit_id"].(map[string]any); ok && byID != nil {
		if c, ok := byID["codex"].(map[string]any); ok && c != nil {
			return c
		}
	}

	return map[string]any{}
}

func extractCodexReviewRateLimit(data map[string]any) map[string]any {
	if rl, ok := data["code_review_rate_limit"].(map[string]any); ok && rl != nil {
		return rl
	}

	if rl, ok := data["review_rate_limit"].(map[string]any); ok && rl != nil {
		return rl
	}

	if rl := extractCodexReviewByID(data); len(rl) > 0 {
		return rl
	}

	return extractCodexReviewAdditional(data)
}

func extractCodexReviewByID(data map[string]any) map[string]any {
	byID, ok := data["rate_limits_by_limit_id"].(map[string]any)
	if !ok || byID == nil {
		return nil
	}

	for _, k := range []string{"code_review", "codex_review", "review"} {
		if v, ok := byID[k].(map[string]any); ok && v != nil {
			return v
		}
	}

	return nil
}

func extractCodexReviewAdditional(data map[string]any) map[string]any {
	addl, ok := data["additional_rate_limits"].([]any)
	if !ok {
		return map[string]any{}
	}

	for _, item := range addl {
		if m, ok := item.(map[string]any); ok && m != nil {
			id := strings.ToLower(fmt.Sprintf("%v", m["limit_name"]))
			if strings.Contains(id, "review") {
				return m
			}
		}
	}

	return map[string]any{}
}

func appendCodexQuotaWindows(quotas map[string]QuotaItem, prefix string, rl map[string]any) {
	if len(rl) == 0 {
		return
	}

	primary := getCodexWindow(rl, "primary_window", "primary")
	secondary := getCodexWindow(rl, "secondary_window", "secondary")

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

func getCodexWindow(rl map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if w, ok := rl[key].(map[string]any); ok && w != nil {
			return w
		}
	}

	return nil
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
		ResetAt:             res,
		Recurring:           nil,
		DisplayName:         "",
		Unit:                "",
		Used:                used,
		Total:               100,
		Remaining:           rem,
		RemainingPercentage: rem,
		Unlimited:           false,
	}
}
