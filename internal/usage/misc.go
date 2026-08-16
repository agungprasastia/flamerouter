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

func fetchIflowUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return &QuotaResult{
		Provider: "iflow",
		Message:  "iFlow connected. Usage tracked per request.",
	}, nil
}

func fetchOllamaUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return &QuotaResult{Message: "Ollama Cloud API key not available."}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ollamaUsageURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Ollama Cloud error: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{Message: "Ollama Cloud API key invalid or expired."}, nil
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{Message: fmt.Sprintf("Ollama Cloud usage API error (%d).", res.StatusCode)}, nil
	}

	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{Message: "Ollama Cloud usage response was not JSON."}, nil
	}

	plan := "Ollama Cloud"

	reqMe, errMe := http.NewRequestWithContext(ctx, http.MethodPost, ollamaMeURL, nil)
	if errMe == nil {
		reqMe.Header.Set("Authorization", "Bearer "+apiKey)
		reqMe.Header.Set("Accept", "application/json")
		reqMe.Header.Set("Content-Length", "0")

		if resMe, errMeDo := doHTTP(opts.HTTPClient, reqMe); errMeDo == nil {
			defer resMe.Body.Close()

			if resMe.StatusCode >= 200 && resMe.StatusCode < 300 {
				var meData struct {
					Plan string `json:"Plan"`
				}

				if json.NewDecoder(resMe.Body).Decode(&meData) == nil && meData.Plan != "" {
					plan = titleCase(meData.Plan)
				}
			}
		}
	}

	limits, _ := data["limits"].(map[string]any)
	quotas := make(map[string]QuotaItem)

	if sess, ok := limits["session"].(map[string]any); ok && sess["usage"] != nil {
		u := toFiniteFloat(sess["usage"], 0)
		ratio := math.Max(0, math.Min(1, u))
		usedPct := math.Round(ratio * 100)
		quotas["Session (5h)"] = QuotaItem{
			Used:                usedPct,
			Total:               100,
			RemainingPercentage: 100 - usedPct,
			Unlimited:           false,
		}
	}

	if weekly, ok := limits["weekly"].(map[string]any); ok && weekly["usage"] != nil {
		u := toFiniteFloat(weekly["usage"], 0)
		ratio := math.Max(0, math.Min(1, u))
		usedPct := math.Round(ratio * 100)
		quotas["Weekly (7d)"] = QuotaItem{
			Used:                usedPct,
			Total:               100,
			RemainingPercentage: 100 - usedPct,
			Unlimited:           false,
		}
	}

	if len(quotas) == 0 {
		return &QuotaResult{
			Provider: "ollama",
			Plan:     plan,
			Message:  "Ollama Cloud connected. No usage limits reported.",
			Quotas:   quotas,
		}, nil
	}

	return &QuotaResult{
		Provider: "ollama",
		Plan:     plan,
		Quotas:   quotas,
	}, nil
}

func fetchGlmUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return queryGlm(ctx, opts, glmIntlURL)
}

func fetchGlmCnUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return queryGlm(ctx, opts, glmCnURL)
}

func queryGlm(ctx context.Context, opts FetchOptions, quotaURL string) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return &QuotaResult{Message: "GLM API key not available."}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, quotaURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("GLM error: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode == 401 {
		return &QuotaResult{Message: "GLM API key invalid or expired."}, nil
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{Message: fmt.Sprintf("GLM quota API error (%d).", res.StatusCode)}, nil
	}

	var jsonMap struct {
		Data struct {
			Level  string `json:"level"`
			Limits []struct {
				Percentage    any    `json:"percentage"`
				NextResetTime any    `json:"nextResetTime"`
				Type          string `json:"type"`
			} `json:"limits"`
		} `json:"data"`
	}

	if err := json.NewDecoder(res.Body).Decode(&jsonMap); err != nil {
		return &QuotaResult{Message: "GLM error: invalid response JSON"}, nil
	}

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
			Used:                usedPct,
			Total:               100,
			Remaining:           rem,
			RemainingPercentage: rem,
			ResetAt:             resetAt,
			Unlimited:           false,
		}
	}

	plan := titleCase(jsonMap.Data.Level)
	if plan == "" {
		plan = "Unknown"
	}

	return &QuotaResult{
		Provider: opts.Provider,
		Plan:     plan,
		Quotas:   quotas,
	}, nil
}
