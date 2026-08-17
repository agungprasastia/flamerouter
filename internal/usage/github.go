package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const githubQuotaURL = "https://api.github.com/copilot_internal/user"

func init() {
	RegisterQuotaHandler("github", fetchGitHubUsage)
}

func parseGitHubQuotaSnapshots(snaps map[string]any, plan string, resetAt *string) *QuotaResult {
	quotas := make(map[string]QuotaItem)

	if chat, ok := snaps["chat"].(map[string]any); ok && chat != nil {
		quotas["chat"] = formatGitHubQuotaSnapshot(chat, resetAt)
	}

	if comp, ok := snaps["completions"].(map[string]any); ok && comp != nil {
		quotas["completions"] = formatGitHubQuotaSnapshot(comp, resetAt)
	}

	if prem, ok := snaps["premium_interactions"].(map[string]any); ok && prem != nil {
		quotas["premium_interactions"] = formatGitHubQuotaSnapshot(prem, resetAt)
	}

	return &QuotaResult{
		Provider:           "github",
		Plan:               plan,
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      0,
		LimitReached:       nil,
		ReviewLimitReached: nil,
		IsQuotaExceeded:    nil,
		ResetCredits:       nil,
		ResetsAt:           resetAt,
		Message:            "",
		Details:            nil,
		Quotas:             quotas,
	}
}

func parseGitHubMonthlyQuotas(data map[string]any, plan string) (*QuotaResult, bool) {
	var monthlyQuotas map[string]any
	if val, ok := data["monthly_quotas"].(map[string]any); ok {
		monthlyQuotas = val
	}

	var usedQuotas map[string]any
	if val, ok := data["limited_user_quotas"].(map[string]any); ok {
		usedQuotas = val
	}

	if monthlyQuotas == nil && usedQuotas == nil {
		return nil, false
	}

	resetAt := parseResetTime(data["limited_user_reset_date"])
	chatTotal := toFiniteFloat(monthlyQuotas["chat"], 0)
	chatUsed := toFiniteFloat(usedQuotas["chat"], 0)
	compTotal := toFiniteFloat(monthlyQuotas["completions"], 0)
	compUsed := toFiniteFloat(usedQuotas["completions"], 0)

	quotas := map[string]QuotaItem{
		"chat":        makeQuota(chatUsed, chatTotal, resetAt, false),
		"completions": makeQuota(compUsed, compTotal, resetAt, false),
	}

	return &QuotaResult{
		Provider:           "github",
		Plan:               plan,
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      0,
		LimitReached:       nil,
		ReviewLimitReached: nil,
		IsQuotaExceeded:    nil,
		ResetCredits:       nil,
		ResetsAt:           resetAt,
		Message:            "",
		Details:            nil,
		Quotas:             quotas,
	}, true
}

func parseGitHubData(data map[string]any) *QuotaResult {
	var plan string
	if val, ok := data["copilot_plan"].(string); ok {
		plan = val
	} else if val, ok := data["access_type_sku"].(string); ok {
		plan = val
	}

	if snaps, ok := data["quota_snapshots"].(map[string]any); ok && snaps != nil {
		resetAt := parseResetTime(data["quota_reset_date"])

		return parseGitHubQuotaSnapshots(snaps, plan, resetAt)
	}

	if res, ok := parseGitHubMonthlyQuotas(data, plan); ok {
		return res
	}

	return &QuotaResult{
		Provider:           "github",
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
		Message:            "GitHub Copilot connected. Unable to parse quota data.",
		Details:            nil,
		Quotas:             nil,
	}
}

func executeGitHubRequest(ctx context.Context, opts FetchOptions) (*http.Response, error) {
	u := githubQuotaURL
	if opts.BaseURL != "" {
		u = opts.BaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "token "+opts.AccessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "GitHubCopilotChat/0.26.7")
	req.Header.Set("Editor-Version", "vscode/1.100.0")
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/0.26.7")

	return doHTTP(opts.HTTPClient, req)
}

func fetchGitHubUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{
			Provider:           "github",
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
			Message:            "No GitHub access token available. Please re-authorize the connection.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	res, err := executeGitHubRequest(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch GitHub usage: %w", err)
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	return handleGitHubResponse(res)
}

func handleGitHubResponse(res *http.Response) (*QuotaResult, error) {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBytes, err := io.ReadAll(io.LimitReader(res.Body, 512))
		if err != nil {
			errBytes = nil
		}

		return &QuotaResult{
			Provider:           "github",
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
			Message:            fmt.Sprintf("Failed to fetch GitHub usage: GitHub API error: %s", strings.TrimSpace(string(errBytes))),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("github copilot connected: unable to parse quota data: %w", err)
	}

	return parseGitHubData(data), nil
}

func formatGitHubQuotaSnapshot(quota map[string]any, resetAt *string) QuotaItem {
	ent := toFiniteFloat(quota["entitlement"], 0)
	rem := toFiniteFloat(quota["remaining"], 0)

	var unlimited bool
	if val, ok := quota["unlimited"].(bool); ok {
		unlimited = val
	}

	used := mathMax(0, ent-rem)

	return makeQuota(used, ent, resetAt, unlimited)
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}

	return b
}
