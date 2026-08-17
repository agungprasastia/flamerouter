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

func parseClaudeOAuthData(data map[string]any) *QuotaResult {
	quotas := make(map[string]QuotaItem)

	extractClaudeFixedQuotas(data, quotas)
	extractClaudeModelQuotas(data, quotas)

	return &QuotaResult{
		Provider:           "claude",
		Plan:               "Claude Code",
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      0,
		LimitReached:       nil,
		ReviewLimitReached: nil,
		IsQuotaExceeded:    nil,
		ResetCredits:       nil,
		ResetsAt:           nil,
		Message:            "",
		Details:            map[string]any{"extraUsage": data["extra_usage"]},
		Quotas:             quotas,
	}
}

func extractClaudeFixedQuotas(data map[string]any, quotas map[string]QuotaItem) {
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
}

func extractClaudeModelQuotas(data map[string]any, quotas map[string]QuotaItem) {
	for k, v := range data {
		if !strings.HasPrefix(k, "seven_day_") || k == "seven_day" {
			continue
		}

		if vm, ok := v.(map[string]any); ok && vm != nil {
			if ut, has := vm["utilization"].(float64); has {
				modelName := strings.TrimPrefix(k, "seven_day_")
				quotas["weekly "+modelName+" (7d)"] = makeClaudeQuotaObject(ut, vm["resets_at"])
			}
		}
	}
}

func isClaudeCoolingDown(token string, force bool) bool {
	claudeCooldownMu.Lock()
	defer claudeCooldownMu.Unlock()

	cooldownUntil, hasCooldown := claudeCooldown[token]

	return !force && hasCooldown && time.Now().Before(cooldownUntil)
}

func setClaudeCooldown(token string) {
	claudeCooldownMu.Lock()
	defer claudeCooldownMu.Unlock()

	claudeCooldown[token] = time.Now().Add(3 * time.Minute)
}

func executeClaudeOAuthRequest(ctx context.Context, opts FetchOptions) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeOAuthUsageURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("anthropic-version", claudeAPIVersion)

	return doHTTP(opts.HTTPClient, req)
}

func fetchClaudeUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{
			Provider:           "claude",
			Plan:               "Claude Code",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            "No Claude access token available.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	if isClaudeCoolingDown(opts.AccessToken, opts.Force) {
		return fetchClaudeUsageLegacy(ctx, opts)
	}

	res, err := executeClaudeOAuthRequest(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("claude connected: unable to fetch usage: %w", err)
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	if res.StatusCode == 429 {
		setClaudeCooldown(opts.AccessToken)

		return fetchClaudeUsageLegacy(ctx, opts)
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fetchClaudeUsageLegacy(ctx, opts)
	}

	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("claude connected: invalid response JSON: %w", err)
	}

	return parseClaudeOAuthData(data), nil
}

func makeClaudeQuotaObject(used float64, resetsAtVal any) QuotaItem {
	rem := math.Max(0, 100.0-used)

	return QuotaItem{
		ResetAt:             parseResetTime(resetsAtVal),
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

func fetchClaudeOrgUsage(ctx context.Context, client *http.Client, token, orgID string) map[string]any {
	if orgID == "" {
		return nil
	}

	usageURL := strings.ReplaceAll(claudeOrgUsageURL, "{org_id}", orgID)

	uReq, err := http.NewRequestWithContext(ctx, http.MethodGet, usageURL, nil)
	if err != nil {
		return nil
	}

	uReq.Header.Set("Authorization", "Bearer "+token)
	uReq.Header.Set("anthropic-version", claudeAPIVersion)

	uRes, err := doHTTP(client, uReq)
	if err != nil {
		return nil
	}

	defer func() {
		if closeErr := uRes.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	if uRes.StatusCode < 200 || uRes.StatusCode >= 300 {
		return nil
	}

	var usageData map[string]any
	if err := json.NewDecoder(uRes.Body).Decode(&usageData); err != nil {
		return nil
	}

	return usageData
}

func fetchClaudeUsageLegacy(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, claudeSettingsURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("anthropic-version", claudeAPIVersion)

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, fmt.Errorf("claude connected: unable to fetch usage: %w", err)
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	return handleClaudeLegacyResponse(ctx, opts, res)
}

func handleClaudeLegacyResponse(ctx context.Context, opts FetchOptions, res *http.Response) (*QuotaResult, error) {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           "claude",
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
			Message:            "Claude connected. Usage API requires admin permissions.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	var settings map[string]any
	if err := json.NewDecoder(res.Body).Decode(&settings); err != nil {
		return nil, fmt.Errorf("claude connected: unable to parse settings JSON: %w", err)
	}

	return buildClaudeLegacyResult(ctx, opts, settings), nil
}

func buildClaudeLegacyResult(ctx context.Context, opts FetchOptions, settings map[string]any) *QuotaResult {
	plan, ok := settings["plan"].(string)
	if !ok || plan == "" {
		plan = "Unknown"
	}

	var orgID string
	if val, ok := settings["organization_id"].(string); ok {
		orgID = val
	}

	if usageData := fetchClaudeOrgUsage(ctx, opts.HTTPClient, opts.AccessToken, orgID); usageData != nil {
		return &QuotaResult{
			Provider:           "claude",
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
			Message:            "",
			Details:            usageData,
			Quotas:             nil,
		}
	}

	return &QuotaResult{
		Provider:           "claude",
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
		Message:            "Claude connected. Usage details require admin access.",
		Details:            nil,
		Quotas:             nil,
	}
}
