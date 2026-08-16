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

func fetchQoderUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	token := opts.AccessToken
	if token == "" {
		token = opts.APIKey
	}

	if token == "" {
		return &QuotaResult{Message: "Qoder usage unavailable: no access token"}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, qoderQuotaURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Qoder connected. Unable to fetch usage: %v", err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{Message: fmt.Sprintf("Qoder connected. Usage fetch returned %d.", res.StatusCode)}, nil
	}

	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return &QuotaResult{Message: "Qoder connected. Usage response was not JSON."}, nil
	}

	userQuota, _ := body["userQuota"].(map[string]any)
	orgQuota, _ := body["orgResourcePackage"].(map[string]any)

	expiresAtMs := int64(toFiniteFloat(body["expiresAt"], 0))

	var resetAt *string

	if expiresAtMs > 0 {
		iso := time.UnixMilli(expiresAtMs).UTC().Format(time.RFC3339Nano)
		resetAt = &iso
	}

	quotas := make(map[string]QuotaItem)

	if userQuota != nil {
		tot := toFiniteFloat(userQuota["total"], 0)
		usd := toFiniteFloat(userQuota["used"], 0)
		rem := toFiniteFloat(userQuota["remaining"], 0)

		unit, _ := userQuota["unit"].(string)
		if unit == "" {
			unit = "credits"
		}

		item := makeQuota(usd, tot, resetAt, false)
		item.Remaining = rem
		item.Unit = unit
		quotas["user"] = item
	}

	if orgQuota != nil {
		tot := toFiniteFloat(orgQuota["total"], 0)
		usd := toFiniteFloat(orgQuota["used"], 0)
		rem := toFiniteFloat(orgQuota["remaining"], 0)

		unit, _ := orgQuota["unit"].(string)
		if unit == "" {
			unit = "credits"
		}

		item := makeQuota(usd, tot, resetAt, false)
		item.Remaining = rem
		item.Unit = unit
		quotas["organization"] = item
	}

	totUsagePct := toFiniteFloat(body["totalUsagePercentage"], 0)

	var isQuotaExceeded *bool
	if ex, ok := body["isQuotaExceeded"].(bool); ok {
		isQuotaExceeded = &ex
	}

	return &QuotaResult{
		Provider:        "qoder",
		Quotas:          quotas,
		TotalUsagePct:   totUsagePct,
		IsQuotaExceeded: isQuotaExceeded,
		ResetsAt:        resetAt,
	}, nil
}
