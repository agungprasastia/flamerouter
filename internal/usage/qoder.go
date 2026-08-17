package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const qoderQuotaURL = "https://api.qoder.com/api/v1/user/quota"

func init() {
	RegisterQuotaHandler("qoder", fetchQoderUsage)
}

func parseQoderQuotaItem(data map[string]any, resetAt *string) QuotaItem {
	tot := toFiniteFloat(data["total"], 0)
	usd := toFiniteFloat(data["used"], 0)
	rem := toFiniteFloat(data["remaining"], 0)

	unit, ok := data["unit"].(string)
	if !ok || unit == "" {
		unit = "credits"
	}

	item := makeQuota(usd, tot, resetAt, false)
	item.Remaining = rem
	item.Unit = unit

	return item
}

func parseQoderBody(body map[string]any) *QuotaResult {
	expiresAtMs := int64(toFiniteFloat(body["expiresAt"], 0))

	var resetAt *string

	if expiresAtMs > 0 {
		iso := time.UnixMilli(expiresAtMs).UTC().Format(time.RFC3339Nano)
		resetAt = &iso
	}

	quotas := make(map[string]QuotaItem)

	if userQuota, ok := body["userQuota"].(map[string]any); ok && userQuota != nil {
		quotas["user"] = parseQoderQuotaItem(userQuota, resetAt)
	}

	if orgQuota, ok := body["orgResourcePackage"].(map[string]any); ok && orgQuota != nil {
		quotas["organization"] = parseQoderQuotaItem(orgQuota, resetAt)
	}

	totUsagePct := toFiniteFloat(body["totalUsagePercentage"], 0)

	var isQuotaExceeded *bool
	if ex, ok := body["isQuotaExceeded"].(bool); ok {
		isQuotaExceeded = &ex
	}

	return &QuotaResult{
		Provider:           "qoder",
		Plan:               "",
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      totUsagePct,
		LimitReached:       nil,
		ReviewLimitReached: nil,
		IsQuotaExceeded:    isQuotaExceeded,
		ResetCredits:       nil,
		ResetsAt:           resetAt,
		Message:            "",
		Details:            nil,
		Quotas:             quotas,
	}
}

func executeQoderRequest(ctx context.Context, opts FetchOptions, token string) (*http.Response, *QuotaResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qoderQuotaURL, http.NoBody)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	res, reqErr := doHTTP(opts.HTTPClient, req)
	if reqErr != nil {
		return nil, &QuotaResult{
			Provider:           "qoder",
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
			Message:            fmt.Sprintf("Qoder connected. Unable to fetch usage: %v", reqErr),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return res, nil, nil
}

func parseQoderResponse(res *http.Response) *QuotaResult {
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           "qoder",
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
			Message:            fmt.Sprintf("Qoder connected. Usage fetch returned %d.", res.StatusCode),
			Details:            nil,
			Quotas:             nil,
		}
	}

	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return &QuotaResult{
			Provider:           "qoder",
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
			Message:            "Qoder response parse error",
			Details:            nil,
			Quotas:             nil,
		}
	}

	return parseQoderBody(body)
}

func fetchQoderUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	token := opts.AccessToken
	if token == "" {
		token = opts.APIKey
	}

	if token == "" {
		return &QuotaResult{
			Provider:           "qoder",
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
			Message:            "Qoder usage unavailable: no access token",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	res, errRes, err := executeQoderRequest(ctx, opts, token)
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

	return parseQoderResponse(res), nil
}
