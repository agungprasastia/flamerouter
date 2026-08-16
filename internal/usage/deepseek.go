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

func fetchDeepseekUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return &QuotaResult{Message: "DeepSeek API key not available. Add a key to view usage."}, nil
	}

	u := deepseekBalanceURL
	if opts.BaseURL != "" {
		u = opts.BaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("DeepSeek error: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{
			Plan:    "DeepSeek",
			Message: "DeepSeek authentication failed. Check the API key.",
		}, nil
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBytes, _ := io.ReadAll(io.LimitReader(res.Body, 512))

		trimmed := strings.TrimSpace(string(errBytes))
		if len(trimmed) > 120 {
			trimmed = trimmed[:120]
		}

		if trimmed != "" {
			return &QuotaResult{
				Plan:    "DeepSeek",
				Message: fmt.Sprintf("DeepSeek balance API error (%d): %s", res.StatusCode, trimmed),
			}, nil
		}

		return &QuotaResult{
			Plan:    "DeepSeek",
			Message: fmt.Sprintf("DeepSeek balance API error (%d)", res.StatusCode),
		}, nil
	}

	var data struct {
		IsAvailable  bool `json:"is_available"`
		IsAvailable2 bool `json:"isAvailable"`
		BalanceInfos []struct {
			Currency       string `json:"currency"`
			TotalBalance   any    `json:"total_balance"`
			TotalBalance2  any    `json:"totalBalance"`
			GrantedBalance any    `json:"granted_balance"`
			ToppedBalance  any    `json:"topped_up_balance"`
		} `json:"balance_infos"`
	}

	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{Message: "DeepSeek balance response was not JSON."}, nil
	}

	if len(data.BalanceInfos) == 0 {
		return &QuotaResult{
			Plan:    "DeepSeek",
			Message: "DeepSeek connected. No balance data returned.",
		}, nil
	}

	isAvail := data.IsAvailable || data.IsAvailable2
	plan := "DeepSeek"

	if !isAvail {
		plan = "DeepSeek (Insufficient Balance)"
	}

	quotas := make(map[string]QuotaItem)

	for _, b := range data.BalanceInfos {
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
			Used:                0,
			Total:               tot,
			RemainingPercentage: remPct,
			Unlimited:           tot > 0,
		}
	}

	return &QuotaResult{
		Provider: "deepseek",
		Plan:     plan,
		Quotas:   quotas,
	}, nil
}
