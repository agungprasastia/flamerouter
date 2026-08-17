package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	ollamaUsageURL = "https://ollama.com/api/usage"
	ollamaMeURL    = "https://ollama.com/api/me"
	glmIntlURL     = "https://api.zhipuai.cn/api/paas/v4/user/quota"
	glmCnURL       = "https://open.bigmodel.cn/api/paas/v4/user/quota"
)

func init() {
	RegisterQuotaHandler("iflow", fetchIflowUsage)
	RegisterQuotaHandler("ollama", fetchOllamaUsage)
	RegisterQuotaHandler("glm", fetchGlmUsage)
	RegisterQuotaHandler("glm-cn", fetchGlmCnUsage)
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}

	return strings.Join(words, " ")
}

func fetchIflowUsage(_ context.Context, _ FetchOptions) (*QuotaResult, error) {
	return &QuotaResult{
		Provider:           "iflow",
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
		Message:            "iFlow connected. Usage tracked per request.",
		Details:            nil,
		Quotas:             nil,
	}, nil
}

func fetchOllamaPlan(ctx context.Context, client *http.Client, apiKey string) string {
	plan := "Ollama Cloud"

	reqMe, errMe := http.NewRequestWithContext(ctx, http.MethodPost, ollamaMeURL, nil)
	if errMe != nil {
		return plan
	}

	reqMe.Header.Set("Authorization", "Bearer "+apiKey)
	reqMe.Header.Set("Accept", "application/json")
	reqMe.Header.Set("Content-Length", "0")

	resMe, errMeDo := doHTTP(client, reqMe)
	if errMeDo != nil {
		return plan
	}

	defer func() {
		if err := resMe.Body.Close(); err != nil {
			_ = err
		}
	}()

	if resMe.StatusCode >= 200 && resMe.StatusCode < 300 {
		var meData struct {
			Plan string `json:"Plan"`
		}

		if json.NewDecoder(resMe.Body).Decode(&meData) == nil && meData.Plan != "" {
			plan = titleCase(meData.Plan)
		}
	}

	return plan
}

func parseOllamaLimits(limits map[string]any) map[string]QuotaItem {
	quotas := make(map[string]QuotaItem)

	if sess, ok := limits["session"].(map[string]any); ok && sess["usage"] != nil {
		u := toFiniteFloat(sess["usage"], 0)
		ratio := math.Max(0, math.Min(1, u))
		usedPct := math.Round(ratio * 100)

		quotas["Session (5h)"] = QuotaItem{
			ResetAt:             nil,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                usedPct,
			Total:               100,
			Remaining:           100 - usedPct,
			RemainingPercentage: 100 - usedPct,
			Unlimited:           false,
		}
	}

	if weekly, ok := limits["weekly"].(map[string]any); ok && weekly["usage"] != nil {
		u := toFiniteFloat(weekly["usage"], 0)
		ratio := math.Max(0, math.Min(1, u))
		usedPct := math.Round(ratio * 100)

		quotas["Weekly (7d)"] = QuotaItem{
			ResetAt:             nil,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                usedPct,
			Total:               100,
			Remaining:           100 - usedPct,
			RemainingPercentage: 100 - usedPct,
			Unlimited:           false,
		}
	}

	return quotas
}

func checkOllamaResponseStatus(res *http.Response) *QuotaResult {
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{
			Provider:           "ollama",
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
			Message:            "Ollama Cloud API key invalid or expired.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           "ollama",
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
			Message:            fmt.Sprintf("Ollama Cloud usage API error (%d).", res.StatusCode),
			Details:            nil,
			Quotas:             nil,
		}
	}

	return nil
}

func parseOllamaUsageResponse(ctx context.Context, opts FetchOptions, res *http.Response, apiKey string) *QuotaResult {
	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{
			Provider:           "ollama",
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
			Message:            fmt.Sprintf("Ollama Cloud usage response was not JSON: %v", err),
			Details:            nil,
			Quotas:             nil,
		}
	}

	plan := fetchOllamaPlan(ctx, opts.HTTPClient, apiKey)

	var limits map[string]any
	if l, ok := data["limits"].(map[string]any); ok {
		limits = l
	}

	quotas := parseOllamaLimits(limits)

	msg := ""
	if len(quotas) == 0 {
		msg = "Ollama Cloud connected. No usage limits reported."
	}

	return &QuotaResult{
		Provider:           "ollama",
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
		Quotas:             quotas,
	}
}

func executeOllamaRequest(ctx context.Context, opts FetchOptions, apiKey string) (*http.Response, *QuotaResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaUsageURL, nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, &QuotaResult{
			Provider:           "ollama",
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
			Message:            fmt.Sprintf("Ollama Cloud error: %v", err),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return res, nil, nil
}

func fetchOllamaUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return &QuotaResult{
			Provider:           "ollama",
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
			Message:            "Ollama Cloud API key not available.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	res, errRes, err := executeOllamaRequest(ctx, opts, apiKey)
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

	if statusRes := checkOllamaResponseStatus(res); statusRes != nil {
		return statusRes, nil
	}

	return parseOllamaUsageResponse(ctx, opts, res, apiKey), nil
}

func fetchGlmUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return queryGlm(ctx, opts, glmIntlURL)
}

func fetchGlmCnUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return queryGlm(ctx, opts, glmCnURL)
}

type glmJSONResponse struct {
	Data struct {
		Level  string `json:"level"`
		Limits []struct {
			Percentage    any    `json:"percentage"`
			NextResetTime any    `json:"nextResetTime"`
			Type          string `json:"type"`
		} `json:"limits"`
	} `json:"data"`
}

func parseGlmQuotas(jsonMap glmJSONResponse, provider string) *QuotaResult {
	quotas := make(map[string]QuotaItem)

	for _, l := range jsonMap.Data.Limits {
		if l.Type != "TOKENS_LIMIT" {
			continue
		}

		usedPct := toFiniteFloat(l.Percentage, 0)
		resetMs := int64(toFiniteFloat(l.NextResetTime, 0))

		var resetAt *string

		if resetMs > 0 {
			iso := time.UnixMilli(resetMs).UTC().Format(time.RFC3339Nano)
			resetAt = &iso
		}

		rem := math.Max(0, 100-usedPct)

		quotas["session"] = QuotaItem{
			ResetAt:             resetAt,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                usedPct,
			Total:               100,
			Remaining:           rem,
			RemainingPercentage: rem,
			Unlimited:           false,
		}
	}

	plan := titleCase(jsonMap.Data.Level)
	if plan == "" {
		plan = "Unknown"
	}

	return &QuotaResult{
		Provider:           provider,
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

func checkGlmResponseStatus(res *http.Response, provider string) *QuotaResult {
	if res.StatusCode == 401 {
		return &QuotaResult{
			Provider:           provider,
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
			Message:            "GLM API key invalid or expired.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           provider,
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
			Message:            fmt.Sprintf("GLM quota API error (%d).", res.StatusCode),
			Details:            nil,
			Quotas:             nil,
		}
	}

	return nil
}

func executeGlmRequest(ctx context.Context, opts FetchOptions, quotaURL string, apiKey string) (*http.Response, *QuotaResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, quotaURL, nil)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, &QuotaResult{
			Provider:           opts.Provider,
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
			Message:            fmt.Sprintf("GLM error: %v", err),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return res, nil, nil
}

func parseGlmResponse(res *http.Response, provider string) *QuotaResult {
	var jsonMap glmJSONResponse
	if err := json.NewDecoder(res.Body).Decode(&jsonMap); err != nil {
		return &QuotaResult{
			Provider:           provider,
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
			Message:            "GLM error: invalid response JSON",
			Details:            nil,
			Quotas:             nil,
		}
	}

	return parseGlmQuotas(jsonMap, provider)
}

func queryGlm(ctx context.Context, opts FetchOptions, quotaURL string) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return &QuotaResult{
			Provider:           opts.Provider,
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
			Message:            "GLM API key not available.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	res, errRes, err := executeGlmRequest(ctx, opts, quotaURL, apiKey)
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

	if statusRes := checkGlmResponseStatus(res, opts.Provider); statusRes != nil {
		return statusRes, nil
	}

	return parseGlmResponse(res, opts.Provider), nil
}
