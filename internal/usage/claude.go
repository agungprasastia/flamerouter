package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	claudeOAuthUsageURL = "https://api.anthropic.com/api/oauth/usage"
	claudeSettingsURL   = "https://api.anthropic.com/v1/settings"
	claudeOrgUsageURL   = "https://api.anthropic.com/v1/organizations/{org_id}/usage"
	claudeAPIVersion    = "2023-06-01"
)

var (
	claudeCooldownMu sync.Mutex
	claudeCooldown   = map[string]time.Time{}
)

func init() {
	RegisterQuotaHandler("claude", fetchClaudeUsage)
}

func fetchClaudeUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{Plan: "Claude Code", Message: "No Claude access token available."}, nil
	}

	claudeCooldownMu.Lock()
	cooldownUntil, hasCooldown := claudeCooldown[opts.AccessToken]
	claudeCooldownMu.Unlock()

	if !opts.Force && hasCooldown && time.Now().Before(cooldownUntil) {
		return fetchClaudeUsageLegacy(ctx, opts)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeOAuthUsageURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("anthropic-version", claudeAPIVersion)

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Claude connected. Unable to fetch usage: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode == 429 {
		claudeCooldownMu.Lock()
		claudeCooldown[opts.AccessToken] = time.Now().Add(3 * time.Minute)
		claudeCooldownMu.Unlock()

		return fetchClaudeUsageLegacy(ctx, opts)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fetchClaudeUsageLegacy(ctx, opts)
	}

	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{Message: "Claude connected. Invalid response JSON."}, nil
	}

	quotas := make(map[string]QuotaItem)

	if fv, ok := data["five_hour"].(map[string]any); ok && fv != nil {
		if ut, has := fv["utilization"].(float64); has {
			quotas["session (5h)"] = makeClaudeQuotaObject(ut, fv["resets_at"])
		}
	}

	if sv, ok := data["seven_day"].(map[string]any); ok && sv != nil {
		if ut, has := sv["utilization"].(float64); has {
			quotas["weekly (7d)"] = makeClaudeQuotaObject(ut, sv["resets_at"])
		}
	}

	for k, v := range data {
		if strings.HasPrefix(k, "seven_day_") && k != "seven_day" {
			if vm, ok := v.(map[string]any); ok && vm != nil {
				if ut, has := vm["utilization"].(float64); has {
					modelName := strings.TrimPrefix(k, "seven_day_")
					quotas["weekly "+modelName+" (7d)"] = makeClaudeQuotaObject(ut, vm["resets_at"])
				}
			}
		}
	}

	return &QuotaResult{
		Provider: "claude",
		Plan:     "Claude Code",
		Quotas:   quotas,
		Details:  map[string]any{"extraUsage": data["extra_usage"]},
	}, nil
}

func makeClaudeQuotaObject(used float64, resetsAtVal any) QuotaItem {
	rem := math.Max(0, 100.0-used)

	return QuotaItem{
		Used:                used,
		Total:               100,
		Remaining:           rem,
		RemainingPercentage: rem,
		ResetAt:             parseResetTime(resetsAtVal),
		Unlimited:           false,
	}
}

func fetchClaudeUsageLegacy(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeSettingsURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("anthropic-version", claudeAPIVersion)

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Claude connected. Unable to fetch usage: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{Message: "Claude connected. Usage API requires admin permissions."}, nil
	}

	var settings map[string]any
	if err := json.NewDecoder(res.Body).Decode(&settings); err != nil {
		return &QuotaResult{Message: "Claude connected. Unable to parse settings JSON."}, nil
	}

	plan, _ := settings["plan"].(string)
	if plan == "" {
		plan = "Unknown"
	}

	orgID, _ := settings["organization_id"].(string)
	if orgID != "" {
		usageURL := strings.ReplaceAll(claudeOrgUsageURL, "{org_id}", orgID)

		uReq, uErr := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
		if uErr == nil {
			uReq.Header.Set("Authorization", "Bearer "+opts.AccessToken)
			uReq.Header.Set("anthropic-version", claudeAPIVersion)

			if uRes, err := opts.HTTPClient.Do(uReq); err == nil {
				defer uRes.Body.Close()

				if uRes.StatusCode >= 200 && uRes.StatusCode < 300 {
					var usageData map[string]any
					if err := json.NewDecoder(uRes.Body).Decode(&usageData); err == nil {
						return &QuotaResult{
							Provider: "claude",
							Plan:     plan,
							Details:  usageData,
						}, nil
					}
				}
			}
		}
	}

	return &QuotaResult{
		Provider: "claude",
		Plan:     plan,
		Message:  "Claude connected. Usage details require admin access.",
	}, nil
}
