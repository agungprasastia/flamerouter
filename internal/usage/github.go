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

func fetchGitHubUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{Message: "No GitHub access token available. Please re-authorize the connection."}, nil
	}

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

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Failed to fetch GitHub usage: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBytes, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return &QuotaResult{Message: fmt.Sprintf("Failed to fetch GitHub usage: GitHub API error: %s", strings.TrimSpace(string(errBytes)))}, nil
	}

	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{Message: "GitHub Copilot connected. Unable to parse quota data."}, nil
	}

	plan, _ := data["copilot_plan"].(string)
	if plan == "" {
		plan, _ = data["access_type_sku"].(string)
	}

	quotas := make(map[string]QuotaItem)

	if snaps, ok := data["quota_snapshots"].(map[string]any); ok && snaps != nil {
		resetAt := parseResetTime(data["quota_reset_date"])
		if chat, ok := snaps["chat"].(map[string]any); ok {
			quotas["chat"] = formatGitHubQuotaSnapshot(chat, resetAt)
		}

		if comp, ok := snaps["completions"].(map[string]any); ok {
			quotas["completions"] = formatGitHubQuotaSnapshot(comp, resetAt)
		}

		if prem, ok := snaps["premium_interactions"].(map[string]any); ok {
			quotas["premium_interactions"] = formatGitHubQuotaSnapshot(prem, resetAt)
		}

		return &QuotaResult{
			Provider: "github",
			Plan:     plan,
			Quotas:   quotas,
			ResetsAt: resetAt,
		}, nil
	}

	monthlyQuotas, _ := data["monthly_quotas"].(map[string]any)
	usedQuotas, _ := data["limited_user_quotas"].(map[string]any)

	if monthlyQuotas != nil || usedQuotas != nil {
		resetAt := parseResetTime(data["limited_user_reset_date"])
		chatTotal := toFiniteFloat(monthlyQuotas["chat"], 0)
		chatUsed := toFiniteFloat(usedQuotas["chat"], 0)
		compTotal := toFiniteFloat(monthlyQuotas["completions"], 0)
		compUsed := toFiniteFloat(usedQuotas["completions"], 0)

		quotas["chat"] = makeQuota(chatUsed, chatTotal, resetAt, false)
		quotas["completions"] = makeQuota(compUsed, compTotal, resetAt, false)

		return &QuotaResult{
			Provider: "github",
			Plan:     plan,
			Quotas:   quotas,
			ResetsAt: resetAt,
		}, nil
	}

	return &QuotaResult{
		Provider: "github",
		Plan:     plan,
		Message:  "GitHub Copilot connected. Unable to parse quota data.",
	}, nil
}

func formatGitHubQuotaSnapshot(quota map[string]any, resetAt *string) QuotaItem {
	ent := toFiniteFloat(quota["entitlement"], 0)
	rem := toFiniteFloat(quota["remaining"], 0)
	unlimited, _ := quota["unlimited"].(bool)
	used := mathMax(0, ent-rem)

	return makeQuota(used, ent, resetAt, unlimited)
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}

	return b
}
