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

const deepseekBalanceURL = "https://api.deepseek.com/user/balance"

func init() {
	RegisterQuotaHandler("deepseek", fetchDeepseekUsage)
}

type deepseekBalanceInfo struct {
	TotalBalance   any    `json:"total_balance"`
	TotalBalance2  any    `json:"totalBalance"`
	GrantedBalance any    `json:"granted_balance"`
	ToppedBalance  any    `json:"topped_up_balance"`
	Currency       string `json:"currency"`
}

type deepseekResponse struct {
	BalanceInfos []deepseekBalanceInfo `json:"balance_infos"`
	IsAvailable  bool                  `json:"is_available"`
	IsAvailable2 bool                  `json:"isAvailable"`
}

func checkDeepseekStatus(res *http.Response) *QuotaResult {
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{
			Provider:           "deepseek",
			Plan:               "DeepSeek",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            "DeepSeek authentication failed. Check the API key.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBytes, err := io.ReadAll(io.LimitReader(res.Body, 512))
		if err != nil {
			errBytes = nil
		}

		trimmed := strings.TrimSpace(string(errBytes))
		if len(trimmed) > 120 {
			trimmed = trimmed[:120]
		}

		msg := fmt.Sprintf("DeepSeek balance API error (%d)", res.StatusCode)
		if trimmed != "" {
			msg = fmt.Sprintf("DeepSeek balance API error (%d): %s", res.StatusCode, trimmed)
		}

		return &QuotaResult{
			Provider:           "deepseek",
			Plan:               "DeepSeek",
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

	return nil
}

func buildDeepseekQuotas(balances []deepseekBalanceInfo) map[string]QuotaItem {
	quotas := make(map[string]QuotaItem)

	for _, b := range balances {
		curr := strings.ToUpper(strings.TrimSpace(b.Currency))
		if curr == "" {
			continue
		}

		tot := toFiniteFloat(b.TotalBalance, math.NaN())
		if math.IsNaN(tot) {
			tot = toFiniteFloat(b.TotalBalance2, 0)
		}

		tot = math.Max(0, tot)
		remPct := 0.0

		if tot > 0 {
			remPct = 100.0
		}

		quotas[fmt.Sprintf("Balance (%s)", curr)] = QuotaItem{
			ResetAt:             nil,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                0,
			Total:               tot,
			Remaining:           tot,
			RemainingPercentage: remPct,
			Unlimited:           tot > 0,
		}
	}

	return quotas
}

func parseDeepseekData(data deepseekResponse) *QuotaResult {
	if len(data.BalanceInfos) == 0 {
		return &QuotaResult{
			Provider:           "deepseek",
			Plan:               "DeepSeek",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            "DeepSeek connected. No balance data returned.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	isAvail := data.IsAvailable || data.IsAvailable2
	plan := "DeepSeek"

	if !isAvail {
		plan = "DeepSeek (Insufficient Balance)"
	}

	quotas := buildDeepseekQuotas(data.BalanceInfos)

	return &QuotaResult{
		Provider:           "deepseek",
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
		Details:            nil,
		Quotas:             quotas,
	}
}

func executeDeepseekRequest(ctx context.Context, opts FetchOptions, apiKey string) (*http.Response, *QuotaResult, error) {
	u := deepseekBalanceURL
	if opts.BaseURL != "" {
		u = opts.BaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, &QuotaResult{
			Provider:           "deepseek",
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
			Message:            fmt.Sprintf("DeepSeek error: %v", err),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return res, nil, nil
}

func parseDeepseekResponseBody(res *http.Response) *QuotaResult {
	var data deepseekResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{
			Provider:           "deepseek",
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
			Message:            "DeepSeek balance response was not JSON.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	return parseDeepseekData(data)
}

func fetchDeepseekUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return &QuotaResult{
			Provider:           "deepseek",
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
			Message:            "DeepSeek API key not available. Add a key to view usage.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	res, errRes, err := executeDeepseekRequest(ctx, opts, apiKey)
	if err != nil {
		return nil, err
	}

	if errRes != nil || res == nil {
		return errRes, nil
	}

	defer func() {
		if res != nil && res.Body != nil {
			if err := res.Body.Close(); err != nil {
				_ = err
			}
		}
	}()

	if statusRes := checkDeepseekStatus(res); statusRes != nil {
		return statusRes, nil
	}

	return parseDeepseekResponseBody(res), nil
}
