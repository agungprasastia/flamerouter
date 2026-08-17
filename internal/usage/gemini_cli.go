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

func resolveGeminiProjectAndPlan(ctx context.Context, client *http.Client, token string, psd map[string]any) (string, string) {
	projectID := extractGoogleProjectID(psd)
	plan := "Free"

	if projectID == "" {
		subInfo := getGoogleSubscriptionInfo(ctx, client, token, geminiLoadCodeAssistURL)
		if subInfo != nil {
			projectID = extractProjectFromSubInfo(subInfo)

			if tierName := extractTierName(subInfo); tierName != "" {
				plan = tierName
			}
		}
	}

	return projectID, plan
}

type geminiBucketResponse struct {
	Buckets []struct {
		ResetTime         any     `json:"resetTime"`
		ModelID           string  `json:"modelId"`
		RemainingFraction float64 `json:"remainingFraction"`
	} `json:"buckets"`
}

func parseGeminiBuckets(data geminiBucketResponse, plan string) *QuotaResult {
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
			ResetAt:             resetAt,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                used,
			Total:               total,
			Remaining:           rem,
			RemainingPercentage: remFrac * 100.0,
			Unlimited:           false,
		}
	}

	return &QuotaResult{
		Provider:           "gemini-cli",
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

func executeGeminiRequest(ctx context.Context, opts FetchOptions, projectID string) (*http.Response, *QuotaResult, error) {
	url := strings.ReplaceAll(geminiQuotaURL, "{project}", projectID)

	reqBody, err := json.Marshal(map[string]string{"project": projectID})
	if err != nil {
		reqBody = []byte("{}")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, &QuotaResult{
			Provider:           "gemini-cli",
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
			Message:            fmt.Sprintf("Gemini CLI error: %v", err),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return res, nil, nil
}

func parseGeminiResponseBody(res *http.Response, plan string) *QuotaResult {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           "gemini-cli",
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
			Message:            fmt.Sprintf("Gemini CLI quota API error (%d).", res.StatusCode),
			Details:            nil,
			Quotas:             nil,
		}
	}

	var data geminiBucketResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{
			Provider:           "gemini-cli",
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
			Message:            "Gemini CLI quota response was not JSON.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	return parseGeminiBuckets(data, plan)
}

func fetchGeminiUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{
			Provider:           "gemini-cli",
			Plan:               "Free",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            "Gemini CLI access token not available.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	projectID, plan := resolveGeminiProjectAndPlan(ctx, opts.HTTPClient, opts.AccessToken, opts.ProviderSpecificData)
	if projectID == "" {
		return &QuotaResult{
			Provider:           "gemini-cli",
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
			Message:            "Gemini CLI project ID not available. Reconnect Gemini CLI, or configure a Google Cloud project with Gemini Code Assist access before checking quota.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	res, errRes, err := executeGeminiRequest(ctx, opts, projectID)
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

	return parseGeminiResponseBody(res, plan), nil
}
