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
	antigravityQuotaURL       = "https://cloudcode-pa.googleapis.com/v1alpha/projects/{project}:retrieveUserQuota"
	antigravityLoadProjectURL = "https://cloudcode-pa.googleapis.com/v1alpha:loadCodeAssist"
)

func init() {
	RegisterQuotaHandler("antigravity", fetchAntigravityUsage)
}

func fetchAntigravityUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{Plan: "Unknown", Message: "Antigravity access token not available."}, nil
	}

	subInfo := getGoogleSubscriptionInfo(ctx, opts.HTTPClient, opts.AccessToken, antigravityLoadProjectURL)
	projectID := extractProjectFromSubInfo(subInfo)
	plan := "Unknown"

	if subInfo != nil {
		if tierName := extractTierName(subInfo); tierName != "" {
			plan = tierName
		}
	}

	url := "https://cloudcode-pa.googleapis.com/v1alpha:retrieveUserQuota"
	if projectID != "" {
		url = strings.ReplaceAll(antigravityQuotaURL, "{project}", projectID)
	}

	reqBodyMap := map[string]any{}
	if projectID != "" {
		reqBodyMap["project"] = projectID
	}

	reqBytes, _ := json.Marshal(reqBodyMap)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("User-Agent", "antigravity/1.0.0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Name", "antigravity")
	req.Header.Set("X-Client-Version", "1.0.0")

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Antigravity error: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode == 403 {
		return &QuotaResult{
			Message: "Antigravity quota API access forbidden. Chat may still work.",
			Quotas:  map[string]QuotaItem{},
		}, nil
	}

	if res.StatusCode == 401 {
		return &QuotaResult{
			Message: "Antigravity quota API authentication expired. Chat may still work.",
			Quotas:  map[string]QuotaItem{},
		}, nil
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{Message: fmt.Sprintf("Antigravity error: API error %d", res.StatusCode)}, nil
	}

	var data struct {
		Models map[string]struct {
			DisplayName string `json:"displayName"`
			IsInternal  bool   `json:"isInternal"`
			QuotaInfo   *struct {
				RemainingFraction float64 `json:"remainingFraction"`
				ResetTime         any     `json:"resetTime"`
			} `json:"quotaInfo"`
		} `json:"models"`
	}

	if err := json.NewDecoder(res.Body).Decode(&data); err != nil {
		return &QuotaResult{Message: "Antigravity error: invalid response JSON"}, nil
	}

	importantModels := map[string]bool{
		"gemini-3.7-flash-high":      true,
		"gemini-3.7-flash-medium":    true,
		"gemini-3.7-flash-low":       true,
		"gemini-3.6-flash-high":      true,
		"gemini-3.6-flash-medium":    true,
		"gemini-3.6-flash-low":       true,
		"gemini-3.5-flash-low":       true,
		"gemini-3.5-flash-extra-low": true,
		"gemini-pro-agent":           true,
		"gemini-3.1-pro-low":         true,
		"claude-sonnet-4-6":          true,
		"claude-opus-4-6-thinking":   true,
		"gpt-oss-120b-medium":        true,
		"gemini-3.1-flash-image":     true,
	}

	quotas := make(map[string]QuotaItem)

	for modelKey, info := range data.Models {
		if info.QuotaInfo == nil || info.IsInternal || !importantModels[modelKey] {
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
			Used:                used,
			Total:               total,
			Remaining:           rem,
			RemainingPercentage: remFrac * 100.0,
			ResetAt:             resetAt,
			DisplayName:         disp,
			Unlimited:           false,
		}
	}

	return &QuotaResult{
		Provider: "antigravity",
		Plan:     plan,
		Quotas:   quotas,
	}, nil
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
	bodyBytes := []byte(`{"metadata":{"ideType":"VSCODE"}}`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return nil
	}

	defer res.Body.Close()

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
