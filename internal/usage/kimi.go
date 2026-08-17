package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
)

const (
	kimiUsageURL = "https://api.kimi.com/coding/v1/usages"
)

var kimiPlanLevels = map[string]string{
	"LEVEL_BASIC":        "Moderato",
	"LEVEL_INTERMEDIATE": "Allegretto",
	"LEVEL_ADVANCED":     "Allegro",
	"LEVEL_STANDARD":     "Vivace",
}

func init() {
	RegisterQuotaHandler("kimi", fetchKimiUsage)
}

func parseKimiLimits(limitsArray []any, quotas map[string]QuotaItem) {
	for _, item := range limitsArray {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		var detail map[string]any
		if val, ok := m["detail"].(map[string]any); ok {
			detail = val
		}

		if detail == nil {
			continue
		}

		lim := toFiniteFloat(getVal(detail, nil, "Limit", "limit"), 0)
		rem := toFiniteFloat(getVal(detail, nil, "Remaining", "remaining"), math.NaN())

		rst := getVal(detail, nil, "ResetTime", "resetTime")
		if rst == nil {
			rst = getVal(detail, nil, "resetAt", "reset_at")
		}

		if lim > 0 {
			resolvedRem := rem
			if math.IsNaN(resolvedRem) {
				resolvedRem = math.Max(0, lim)
			}

			used := math.Max(0, lim-resolvedRem)
			quotas["Ratelimit"] = makeKimiQuota(used, lim, resolvedRem, parseResetTime(rst))
		}
	}
}

func extractKimiPlan(userObj map[string]any) string {
	mem, ok := userObj["membership"].(map[string]any)
	if !ok || mem == nil {
		return "Kimi Coding"
	}

	lvl, ok := mem["level"].(string)
	if !ok || lvl == "" {
		return "Kimi Coding"
	}

	if mapped, has := kimiPlanLevels[lvl]; has {
		return mapped
	}

	return strings.ToLower(strings.TrimPrefix(lvl, "LEVEL_"))
}

func parseKimiData(data map[string]any) *QuotaResult {
	quotas := make(map[string]QuotaItem)

	var usageObj map[string]any
	if val, ok := data["usage"].(map[string]any); ok {
		usageObj = val
	}

	if usageObj != nil {
		lim := toFiniteFloat(getVal(usageObj, nil, "Limit", "limit"), 0)
		usd := toFiniteFloat(getVal(usageObj, nil, "Used", "used"), 0)
		rem := toFiniteFloat(getVal(usageObj, nil, "Remaining", "remaining"), math.NaN())

		rst := getVal(usageObj, nil, "ResetTime", "resetTime")
		if rst == nil {
			rst = getVal(usageObj, nil, "resetAt", "reset_at")
		}

		if lim > 0 {
			quotas["Weekly"] = makeKimiQuota(usd, lim, rem, parseResetTime(rst))
		}
	}

	var limitsArray []any
	if val, ok := data["limits"].([]any); ok {
		limitsArray = val
	}

	if limitsArray != nil {
		parseKimiLimits(limitsArray, quotas)
	}

	planName := "Kimi Coding"
	if userObj, ok := data["user"].(map[string]any); ok && userObj != nil {
		planName = extractKimiPlan(userObj)
	}

	msg := ""
	if len(quotas) == 0 {
		msg = "Kimi Coding connected. Usage tracked per request."
	}

	return &QuotaResult{
		Provider:           "kimi",
		Plan:               planName,
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      0,
		LimitReached:       nil,
		ReviewLimitReached: nil,
		IsQuotaExceeded:    nil,
		ResetCredits:       nil,
		ResetsAt:           nil,
		Message:            msg,
		Details:            nil,
		Quotas:             quotas,
	}
}

func setKimiAuthHeaders(req *http.Request, opts FetchOptions) {
	if opts.APIKey != "" {
		req.Header.Set("x-api-key", opts.APIKey)

		return
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)

	if opts.ProviderSpecificData == nil {
		return
	}

	if devID, ok := opts.ProviderSpecificData["deviceId"].(string); ok && devID != "" {
		req.Header.Set("X-Msh-Device-Id", devID)
	}
}

func buildKimiRequest(ctx context.Context, opts FetchOptions) (*http.Request, error) {
	u := kimiUsageURL
	if opts.BaseURL != "" {
		u = opts.BaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	setKimiAuthHeaders(req, opts)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return req, nil
}

func makeKimiErrorResult(plan, msg string) *QuotaResult {
	return &QuotaResult{
		Provider:           "kimi",
		Plan:               plan,
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      0,
		LimitReached:       nil,
		ReviewLimitReached: nil,
		IsQuotaExceeded:    nil,
		ResetCredits:       nil,
		ResetsAt:           nil,
		Message:            msg,
		Details:            nil,
		Quotas:             nil,
	}
}

func fetchKimiUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.APIKey == "" && opts.AccessToken == "" {
		return makeKimiErrorResult("", "Kimi access token or API key not available."), nil
	}

	req, err := buildKimiRequest(ctx, opts)
	if err != nil {
		return nil, err
	}

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return makeKimiErrorResult("", fmt.Sprintf("Kimi Coding connected. Unable to fetch usage: %v", err)), nil
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	bodyBytes, readErr := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if readErr != nil {
		bodyBytes = nil
	}

	respText := string(bodyBytes)

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return makeKimiErrorResult("Kimi Coding", formatKimiUsageError(res.StatusCode, respText)), nil
	}

	var data map[string]any
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return makeKimiErrorResult("Kimi Coding", "Kimi Coding connected. Invalid JSON response from API."), nil
	}

	return parseKimiData(data), nil
}

func makeKimiQuota(used, total, rem float64, resetAt *string) QuotaItem {
	safeTot := math.Max(0, total)
	safeUsd := math.Max(0, used)

	remPct := 0.0
	if safeTot > 0 && !math.IsNaN(rem) {
		remPct = (math.Max(0, rem) / safeTot) * 100.0
	} else if safeTot > 0 {
		remPct = (math.Max(0, safeTot-safeUsd) / safeTot) * 100.0
	}

	remaining := safeTot - safeUsd
	if !math.IsNaN(rem) {
		remaining = rem
	}

	return QuotaItem{
		ResetAt:             resetAt,
		Recurring:           nil,
		DisplayName:         "",
		Unit:                "",
		Used:                safeUsd,
		Total:               safeTot,
		Remaining:           remaining,
		RemainingPercentage: remPct,
		Unlimited:           false,
	}
}

func extractKimiErrorDetails(parsed map[string]any) (string, string) {
	if parsed == nil {
		return "", ""
	}

	var details []any
	if val, ok := parsed["details"].([]any); ok {
		details = val
	}

	if len(details) == 0 {
		return "", ""
	}

	d0, ok := details[0].(map[string]any)
	if !ok || d0 == nil {
		return "", ""
	}

	return extractKimiDebugDetails(d0)
}

func extractKimiDebugDetails(d0 map[string]any) (string, string) {
	reason := ""
	localized := ""

	var dbg map[string]any
	if val, ok := d0["debug"].(map[string]any); ok {
		dbg = val
	}

	if dbg != nil {
		if val, ok := dbg["reason"].(string); ok {
			reason = val
		}

		localized = extractKimiLocalizedMessage(dbg)
	}

	if localized == "" {
		localized = extractKimiLocalizedMessage(d0)
	}

	return reason, localized
}

func extractKimiLocalizedMessage(m map[string]any) string {
	var loc map[string]any
	if val, ok := m["localizedMessage"].(map[string]any); ok {
		loc = val
	}

	if loc != nil {
		if val, ok := loc["message"].(string); ok {
			return val
		}
	}

	return ""
}

func checkKimiPermissionError(status int, reason, localized, respText string, parsed map[string]any) (string, bool) {
	if status != 403 {
		return "", false
	}

	codeVal := ""
	if parsed != nil {
		codeVal = fmt.Sprintf("%v", parsed["code"])
	}

	combined := strings.ToLower(codeVal + " " + localized + " " + respText)
	if reason == "REASON_FEATURE_NO_PERMISSION" || strings.Contains(combined, "permission") {
		if localized != "" {
			return localized, true
		}

		return "Kimi connected, but this account has no permission to view usage. Subscribe to Kimi Code to access quota.", true
	}

	return "", false
}

func extractKimiTopLevelError(parsed map[string]any) (string, string) {
	if parsed == nil {
		return "", ""
	}

	reason := ""
	if val, ok := parsed["reason"].(string); ok {
		reason = val
	}

	localized := ""
	if val, ok := parsed["message"].(string); ok {
		localized = val
	}

	return reason, localized
}

func formatKimiUsageError(status int, responseText string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(responseText), &parsed); err != nil {
		parsed = nil
	}

	reason, localized := extractKimiErrorDetails(parsed)
	topReason, topLocalized := extractKimiTopLevelError(parsed)

	if reason == "" {
		reason = topReason
	}

	if localized == "" {
		localized = topLocalized
	}

	if status == 401 {
		return "Kimi authentication expired. Please re-authorize."
	}

	if permMsg, isPerm := checkKimiPermissionError(status, reason, localized, responseText, parsed); isPerm {
		return permMsg
	}

	return buildKimiErrorMessage(status, localized, responseText)
}

func buildKimiErrorMessage(status int, localized, responseText string) string {
	snippet := localized
	if snippet == "" {
		snippet = responseText
	}

	if len(snippet) > 100 {
		snippet = snippet[:100]
	}

	if snippet != "" {
		return fmt.Sprintf("Kimi Coding connected. API Error %d: %s", status, snippet)
	}

	return fmt.Sprintf("Kimi Coding connected. API Error %d", status)
}
