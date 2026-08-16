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

func fetchVercelUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return &QuotaResult{Message: "Vercel AI Gateway API key not available."}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.gateway.ai.cloudflare.com/v1/credits", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Vercel AI Gateway error: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{Message: "Vercel AI Gateway API key invalid or expired."}, nil
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBytes, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return &QuotaResult{Message: fmt.Sprintf("Vercel AI Gateway credits API error (%d): %s", res.StatusCode, strings.TrimSpace(string(errBytes)))}, nil
	}

	var data struct {
		Balance   any `json:"balance"`
		TotalUsed any `json:"total_used"`
	}

	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{Message: "Vercel AI Gateway error: invalid response JSON"}, nil
	}

	balance := toFiniteFloat(data.Balance, 0)
	totalUsed := toFiniteFloat(data.TotalUsed, 0)
	monthlyCredit := 5.0
	remPct := (balance / monthlyCredit) * 100.0

	if balance <= 0 && totalUsed <= 0 {
		return &QuotaResult{
			Provider: "vercel-ai-gateway",
			Plan:     "Pay-as-you-go",
			Message:  "Vercel AI Gateway connected. No credit allocation found (BYOK or unfunded account).",
			Quotas:   map[string]QuotaItem{},
		}, nil
	}

	quotas := map[string]QuotaItem{
		"Used (USD)": {
			Used:                totalUsed,
			Total:               0,
			RemainingPercentage: 100,
			Unlimited:           true,
		},
		"Remaining (USD)": {
			Used:                balance,
			Total:               monthlyCredit,
			Remaining:           balance,
			RemainingPercentage: remPct,
			Unlimited:           false,
		},
	}

	return &QuotaResult{
		Provider: "vercel-ai-gateway",
		Plan:     "Pay-as-you-go",
		Quotas:   quotas,
	}, nil
}
