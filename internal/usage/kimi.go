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

func fetchKimiUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	useAPIKey := opts.APIKey != ""
	useOAuth := !useAPIKey && opts.AccessToken != ""

	if !useAPIKey && !useOAuth {
		return &QuotaResult{Message: "Kimi access token or API key not available."}, nil
	}

	u := kimiUsageURL
	if opts.BaseURL != "" {
		u = opts.BaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	if useAPIKey {
		req.Header.Set("x-api-key", opts.APIKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+opts.AccessToken)

		if opts.ProviderSpecificData != nil {
			if devID, ok := opts.ProviderSpecificData["deviceId"].(string); ok && devID != "" {
				req.Header.Set("X-Msh-Device-Id", devID)
			}
		}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Kimi Coding connected. Unable to fetch usage: %v", err)}, nil
	}
	defer res.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	respText := string(bodyBytes)

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Plan:    "Kimi Coding",
			Message: formatKimiUsageError(res.StatusCode, respText),
		}, nil
	}

	var data map[string]any
	if err := json.Unmarshal(bodyBytes, &data); err != nil {
		return &QuotaResult{
			Plan:    "Kimi Coding",
			Message: "Kimi Coding connected. Invalid JSON response from API.",
		}, nil
	}

	quotas := make(map[string]QuotaItem)

	usageObj, _ := data["usage"].(map[string]any)
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

	limitsArray, _ := data["limits"].([]any)
	for _, item := range limitsArray {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		detail, _ := m["detail"].(map[string]any)
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

	planName := "Kimi Coding"

	if userObj, ok := data["user"].(map[string]any); ok {
		if mem, ok := userObj["membership"].(map[string]any); ok {
			if lvl, ok := mem["level"].(string); ok && lvl != "" {
				if mapped, has := kimiPlanLevels[lvl]; has {
					planName = mapped
				} else {
					planName = strings.ToLower(strings.TrimPrefix(lvl, "LEVEL_"))
				}
			}
		}
	}

	if len(quotas) > 0 {
		return &QuotaResult{
			Provider: "kimi",
			Plan:     planName,
			Quotas:   quotas,
		}, nil
	}

	return &QuotaResult{
		Provider: "kimi",
		Plan:     planName,
		Message:  "Kimi Coding connected. Usage tracked per request.",
	}, nil
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

	return QuotaItem{
		Used:                safeUsd,
		Total:               safeTot,
		RemainingPercentage: remPct,
		ResetAt:             resetAt,
		Unlimited:           false,
	}
}

func formatKimiUsageError(status int, responseText string) string {
	var parsed map[string]any
	_ = json.Unmarshal([]byte(responseText), &parsed)

	var reason string

	var localized string

	if details, ok := parsed["details"].([]any); ok && len(details) > 0 {
		if d0, ok := details[0].(map[string]any); ok {
			if dbg, ok := d0["debug"].(map[string]any); ok {
				reason, _ = dbg["reason"].(string)
				if loc, ok := dbg["localizedMessage"].(map[string]any); ok {
					localized, _ = loc["message"].(string)
				}
			}

			if localized == "" {
				if loc, ok := d0["localizedMessage"].(map[string]any); ok {
					localized, _ = loc["message"].(string)
				}
			}
		}
	}

	if reason == "" {
		reason, _ = parsed["reason"].(string)
	}

	if localized == "" {
		localized, _ = parsed["message"].(string)
	}

	if status == 401 {
		return "Kimi authentication expired. Please re-authorize."
	}

	if status == 403 {
		if reason == "REASON_FEATURE_NO_PERMISSION" || strings.Contains(strings.ToLower(fmt.Sprintf("%v %s %s", parsed["code"], localized, responseText)), "permission") {
			if localized != "" {
				return localized
			}

			return "Kimi connected, but this account has no permission to view usage. Subscribe to Kimi Code to access quota."
		}
	}

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
