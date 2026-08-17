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
	antigravityQuotaURL       = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"
	antigravityLoadProjectURL = "https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist"
	antigravityUserAgent      = "antigravity/ide/2.1.1 darwin/arm64"
	antigravityVersion        = "2.1.1"
)

func init() {
	RegisterQuotaHandler("antigravity", fetchAntigravityUsage)
}

type antigravityQuotaInfo struct {
	ResetTime         any     `json:"resetTime"`
	RemainingFraction float64 `json:"remainingFraction"`
}

type antigravityModelInfo struct {
	QuotaInfo   *antigravityQuotaInfo `json:"quotaInfo"`
	DisplayName string                `json:"displayName"`
	IsInternal  bool                  `json:"isInternal"`
}

type antigravityResponse struct {
	Models map[string]antigravityModelInfo `json:"models"`
}

func isImportantAntigravityModel(modelKey string) bool {
	switch modelKey {
	case "gemini-3.7-flash-high", "gemini-3.7-flash-medium", "gemini-3.7-flash-low",
		"gemini-3.6-flash-high", "gemini-3.6-flash-medium", "gemini-3.6-flash-low",
		"gemini-3.5-flash-low", "gemini-3.5-flash-extra-low", "gemini-pro-agent",
		"gemini-3.1-pro-low", "claude-sonnet-4-6", "claude-opus-4-6-thinking",
		"gpt-oss-120b-medium", "gemini-3.1-flash-image":
		return true
	default:
		return false
	}
}

func parseAntigravityModels(data antigravityResponse, plan string) *QuotaResult {
	quotas := make(map[string]QuotaItem)

	for modelKey, info := range data.Models {
		if info.QuotaInfo == nil || info.IsInternal || !isImportantAntigravityModel(modelKey) {
			continue
		}

		remFrac := info.QuotaInfo.RemainingFraction
		total := 1000.0
		rem := math.Round(total * remFrac)
		used := total - rem
		resetAt := parseResetTime(info.QuotaInfo.ResetTime)

		disp := info.DisplayName
		if disp == "" {
			disp = modelKey
		}

		quotas[modelKey] = QuotaItem{
			ResetAt:             resetAt,
			Recurring:           nil,
			DisplayName:         disp,
			Unit:                "",
			Used:                used,
			Total:               total,
			Remaining:           rem,
			RemainingPercentage: remFrac * 100.0,
			Unlimited:           false,
		}
	}

	return &QuotaResult{
		Provider:           "antigravity",
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

func checkAntigravityStatus(res *http.Response) *QuotaResult {
	if res.StatusCode == 403 {
		return &QuotaResult{
			Provider:           "antigravity",
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
			Message:            "Antigravity quota API access forbidden. Chat may still work.",
			Details:            nil,
			Quotas:             map[string]QuotaItem{},
		}
	}

	if res.StatusCode == 401 {
		return &QuotaResult{
			Provider:           "antigravity",
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
			Message:            "Antigravity quota API authentication expired. Chat may still work.",
			Details:            nil,
			Quotas:             map[string]QuotaItem{},
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           "antigravity",
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
			Message:            fmt.Sprintf("Antigravity error: API error %d", res.StatusCode),
			Details:            nil,
			Quotas:             nil,
		}
	}

	return nil
}

func executeAntigravityRequest(ctx context.Context, opts FetchOptions, projectID string) (*http.Response, *QuotaResult, error) {
	reqBodyMap := map[string]any{}
	if projectID != "" {
		reqBodyMap["project"] = projectID
	}

	reqBytes, err := json.Marshal(reqBodyMap)
	if err != nil {
		reqBytes = []byte("{}")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, antigravityQuotaURL, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("User-Agent", antigravityUserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Name", "antigravity")
	req.Header.Set("X-Client-Version", antigravityVersion)

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, &QuotaResult{
			Provider:           "antigravity",
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
			Message:            fmt.Sprintf("Antigravity error: %v", err),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return res, nil, nil
}

func determineAntigravityPlan(ctx context.Context, opts FetchOptions) (string, string) {
	subInfo := getGoogleSubscriptionInfo(ctx, opts.HTTPClient, opts.AccessToken, antigravityLoadProjectURL)
	projectID := extractProjectFromSubInfo(subInfo)
	plan := "Unknown"

	if subInfo != nil {
		if tierName := extractTierName(subInfo); tierName != "" {
			plan = tierName
		}
	}

	return projectID, plan
}

func parseAntigravityResponse(res *http.Response, plan string) *QuotaResult {
	var data antigravityResponse
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{
			Provider:           "antigravity",
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
			Message:            "Antigravity error: invalid response JSON",
			Details:            nil,
			Quotas:             nil,
		}
	}

	return parseAntigravityModels(data, plan)
}

func fetchAntigravityUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{
			Provider:           "antigravity",
			Plan:               "Unknown",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            "Antigravity access token not available.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	projectID, plan := determineAntigravityPlan(ctx, opts)

	res, errRes, err := executeAntigravityRequest(ctx, opts, projectID)
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

	if statusRes := checkAntigravityStatus(res); statusRes != nil {
		return statusRes, nil
	}

	return parseAntigravityResponse(res, plan), nil
}

func extractGoogleProjectID(psd map[string]any) string {
	if psd == nil {
		return ""
	}

	if p, ok := psd["projectId"].(string); ok && strings.TrimSpace(p) != "" {
		return strings.TrimSpace(p)
	}

	if p, ok := psd["project"].(string); ok && strings.TrimSpace(p) != "" {
		return strings.TrimSpace(p)
	}

	return ""
}

func getGoogleSubscriptionInfo(ctx context.Context, client *http.Client, token, url string) map[string]any {
	bodyBytes := []byte(`{"metadata":{"ideType":9,"platform":1,"pluginType":2},"mode":1}`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", antigravityUserAgent)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Name", "antigravity")
	req.Header.Set("X-Client-Version", antigravityVersion)

	res, err := doHTTP(client, req)
	if err != nil {
		return nil
	}

	defer func() {
		if err := res.Body.Close(); err != nil {
			_ = err
		}
	}()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil
	}

	var data map[string]any
	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return nil
	}

	return data
}

func extractProjectFromSubInfo(subInfo map[string]any) string {
	if subInfo == nil {
		return ""
	}

	if p, ok := subInfo["cloudaicompanionProject"].(string); ok && strings.TrimSpace(p) != "" {
		return strings.TrimSpace(p)
	}

	if pObj, ok := subInfo["cloudaicompanionProject"].(map[string]any); ok {
		if id, ok := pObj["id"].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}

	return ""
}

func extractTierName(subInfo map[string]any) string {
	if subInfo == nil {
		return ""
	}

	if ct, ok := subInfo["currentTier"].(map[string]any); ok {
		if name, ok := ct["name"].(string); ok && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}

	return ""
}
