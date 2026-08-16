package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
)

const (
	geminiQuotaURL          = "https://cloudcode-pa.googleapis.com/v1alpha/projects/{project}:retrieveUserQuota"
	geminiLoadCodeAssistURL = "https://cloudcode-pa.googleapis.com/v1alpha:loadCodeAssist"
)

func init() {
	RegisterQuotaHandler("gemini-cli", fetchGeminiUsage)
}

func fetchGeminiUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{Plan: "Free", Message: "Gemini CLI access token not available."}, nil
	}

	projectID := extractGoogleProjectID(opts.ProviderSpecificData)
	plan := "Free"

	if projectID == "" {
		subInfo := getGoogleSubscriptionInfo(ctx, opts.HTTPClient, opts.AccessToken, geminiLoadCodeAssistURL)
		if subInfo != nil {
			projectID = extractProjectFromSubInfo(subInfo)

			if tierName := extractTierName(subInfo); tierName != "" {
				plan = tierName
			}
		}
	}

	if projectID == "" {
		return &QuotaResult{
			Plan:    plan,
			Message: "Gemini CLI project ID not available. Reconnect Gemini CLI, or configure a Google Cloud project with Gemini Code Assist access before checking quota.",
		}, nil
	}

	url := strings.ReplaceAll(geminiQuotaURL, "{project}", projectID)
	reqBody, _ := json.Marshal(map[string]string{"project": projectID})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Gemini CLI error: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{Plan: plan, Message: fmt.Sprintf("Gemini CLI quota error (%d).", res.StatusCode)}, nil
	}

	var data struct {
		Buckets []struct {
			ResetTime         any     `json:"resetTime"`
			ModelID           string  `json:"modelId"`
			RemainingFraction float64 `json:"remainingFraction"`
		} `json:"buckets"`
	}

	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{Plan: plan, Message: "Gemini CLI error: invalid response JSON"}, nil
	}

	quotas := make(map[string]QuotaItem)

	for _, b := range data.Buckets {
		if b.ModelID == "" {
			continue
		}

		remFrac := b.RemainingFraction
		total := 1000.0
		rem := math.Round(total * remFrac)
		used := math.Max(0, total-rem)
		resetAt := parseResetTime(b.ResetTime)
		quotas[b.ModelID] = QuotaItem{
			Used:                used,
			Total:               total,
			Remaining:           rem,
			RemainingPercentage: remFrac * 100.0,
			ResetAt:             resetAt,
			Unlimited:           false,
		}
	}

	return &QuotaResult{
		Provider: "gemini-cli",
		Plan:     plan,
		Quotas:   quotas,
	}, nil
}
