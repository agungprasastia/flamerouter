package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func init() {
	RegisterQuotaHandler("vercel-ai-gateway", fetchVercelUsage)
}

func checkVercelResponseStatus(res *http.Response) *QuotaResult {
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{
			Provider:           "vercel-ai-gateway",
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
			Message:            "Vercel AI Gateway API key invalid or expired.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBytes, err := io.ReadAll(io.LimitReader(res.Body, 512))
		if err != nil {
			errBytes = nil
		}

		return &QuotaResult{
			Provider:           "vercel-ai-gateway",
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
			Message:            fmt.Sprintf("Vercel AI Gateway credits API error (%d): %s", res.StatusCode, strings.TrimSpace(string(errBytes))),
			Details:            nil,
			Quotas:             nil,
		}
	}

	return nil
}

func buildVercelQuotas(balance, totalUsed float64) map[string]QuotaItem {
	monthlyCredit := 5.0
	remPct := (balance / monthlyCredit) * 100.0

	return map[string]QuotaItem{
		"Used (USD)": {
			ResetAt:             nil,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                totalUsed,
			Total:               0,
			Remaining:           0,
			RemainingPercentage: 100,
			Unlimited:           true,
		},
		"Remaining (USD)": {
			ResetAt:             nil,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                balance,
			Total:               monthlyCredit,
			Remaining:           balance,
			RemainingPercentage: remPct,
			Unlimited:           false,
		},
	}
}

func buildVercelResult(balance, totalUsed float64) *QuotaResult {
	if balance <= 0 && totalUsed <= 0 {
		return &QuotaResult{
			Provider:           "vercel-ai-gateway",
			Plan:               "Pay-as-you-go",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            "Vercel AI Gateway connected. No credit allocation found (BYOK or unfunded account).",
			Details:            nil,
			Quotas:             map[string]QuotaItem{},
		}
	}

	return &QuotaResult{
		Provider:           "vercel-ai-gateway",
		Plan:               "Pay-as-you-go",
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
		Details:            nil,
		Quotas:             buildVercelQuotas(balance, totalUsed),
	}
}

func executeVercelRequest(ctx context.Context, opts FetchOptions, apiKey string) (*http.Response, *QuotaResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.gateway.ai.cloudflare.com/v1/credits", nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, &QuotaResult{
			Provider:           "vercel-ai-gateway",
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
			Message:            fmt.Sprintf("Vercel AI Gateway error: %v", err),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return res, nil, nil
}

func parseVercelResponse(res *http.Response) *QuotaResult {
	if statusRes := checkVercelResponseStatus(res); statusRes != nil {
		return statusRes
	}

	var data struct {
		Balance   any `json:"balance"`
		TotalUsed any `json:"total_used"`
	}

	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{
			Provider:           "vercel-ai-gateway",
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
			Message:            "Vercel AI Gateway error: invalid response JSON",
			Details:            nil,
			Quotas:             nil,
		}
	}

	balance := toFiniteFloat(data.Balance, 0)
	totalUsed := toFiniteFloat(data.TotalUsed, 0)

	return buildVercelResult(balance, totalUsed)
}

func fetchVercelUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	key := opts.APIKey
	if key == "" {
		key = opts.AccessToken
	}

	if key == "" {
		res := &QuotaResult{
			Provider:           "vercel-ai-gateway",
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
			Message:            "Vercel AI Gateway API key not available.",
			Details:            nil,
			Quotas:             nil,
		}

		return res, nil
	}

	res, errRes, err := executeVercelRequest(ctx, opts, key)
	if err != nil {
		return nil, err
	}

	if errRes != nil || res == nil {
		return errRes, nil
	}

	defer func() {
		if res != nil && res.Body != nil {
			if closeErr := res.Body.Close(); closeErr != nil {
				_ = closeErr
			}
		}
	}()

	return parseVercelResponse(res), nil
}
