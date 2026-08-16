package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"
)

const (
	codebuddyCnQuotaURL   = "https://copilot.tencent.com/api/v1/user/account_package_details"
	codebuddyIntlQuotaURL = "https://copilot.tencent.com/api/v1/user/account_package_details"
)

func init() {
	RegisterQuotaHandler("codebuddy-cn", fetchCodebuddyCnUsage)
	RegisterQuotaHandler("codebuddy-intl", fetchCodebuddyIntlUsage)
}

func fetchCodebuddyCnUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return queryCodebuddy(ctx, opts, "codebuddy-cn", codebuddyCnQuotaURL)
}

func fetchCodebuddyIntlUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return queryCodebuddy(ctx, opts, "codebuddy-intl", codebuddyIntlQuotaURL)
}

func queryCodebuddy(ctx context.Context, opts FetchOptions, providerId, quotaURL string) (*QuotaResult, error) {
	token := opts.AccessToken
	if token == "" {
		token = opts.APIKey
	}

	if token == "" {
		return &QuotaResult{Message: fmt.Sprintf("CodeBuddy (%s) credential not available.", providerId)}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quotaURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("CodeBuddy (%s) error: %v", providerId, err)}, nil
	}
	defer res.Body.Close()

	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{Message: "CodeBuddy CN credential invalid or expired."}, nil
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{Message: fmt.Sprintf("CodeBuddy CN quota API error (%d).", res.StatusCode)}, nil
	}

	var jsonBody struct {
		Msg  string `json:"msg"`
		Data struct {
			Response struct {
				Data struct {
					Accounts []map[string]any `json:"Accounts"`
				} `json:"Data"`
			} `json:"Response"`
		} `json:"data"`
		Code int `json:"code"`
	}

	if err := json.NewDecoder(res.Body).Decode(&jsonBody); err != nil {
		return &QuotaResult{Message: "CodeBuddy CN quota response was not JSON."}, nil
	}

	if jsonBody.Code != 0 {
		msg := jsonBody.Msg
		if msg == "" {
			msg = "unknown"
		}

		return &QuotaResult{Message: fmt.Sprintf("CodeBuddy CN quota error: %s", msg)}, nil
	}

	accounts := jsonBody.Data.Response.Data.Accounts
	if len(accounts) == 0 {
		return &QuotaResult{Message: "CodeBuddy CN connected. No credit package found."}, nil
	}

	cycleEndMs := func(acc map[string]any) float64 {
		r := parseResetTime(acc["CycleEndTime"])
		if r == nil {
			return math.Inf(1)
		}

		t, err := time.Parse(time.RFC3339Nano, *r)
		if err != nil {
			return math.Inf(1)
		}

		return float64(t.UnixMilli())
	}

	const refillGapMs = 2.0 * 24.0 * 60.0 * 60.0 * 1000.0

	isRefill := func(acc map[string]any) bool {
		ce := cycleEndMs(acc)
		de := toFiniteFloat(acc["DeductionEndTime"], math.NaN())

		return !math.IsInf(ce, 0) && !math.IsNaN(de) && (de-ce > refillGapMs)
	}

	var refills []map[string]any

	var bonuses []map[string]any

	for _, acc := range accounts {
		if isRefill(acc) {
			refills = append(refills, acc)
		} else {
			bonuses = append(bonuses, acc)
		}
	}

	sort.Slice(refills, func(i, j int) bool {
		return cycleEndMs(refills[i]) < cycleEndMs(refills[j])
	})
	sort.Slice(bonuses, func(i, j int) bool {
		return cycleEndMs(bonuses[i]) < cycleEndMs(bonuses[j])
	})

	quotas := make(map[string]QuotaItem)
	seenRefill := make(map[string]int)

	isRecurringTrue := true
	isRecurringFalse := false

	for _, acc := range refills {
		base := refillCadence(acc)
		seenRefill[base]++

		name := base
		if seenRefill[base] > 1 {
			name = fmt.Sprintf("%s %d", base, seenRefill[base])
		}

		used := toFiniteFloat(acc["CycleCapacityUsedPrecise"], math.NaN())
		if math.IsNaN(used) {
			used = toFiniteFloat(acc["CycleCapacityUsed"], 0)
		}

		total := toFiniteFloat(acc["CycleCapacitySizePrecise"], math.NaN())
		if math.IsNaN(total) {
			total = toFiniteFloat(acc["CycleCapacitySize"], 0)
		}

		item := makeQuota(used, total, parseResetTime(acc["CycleEndTime"]), false)
		item.Recurring = &isRecurringTrue
		quotas[name] = item
	}

	for i, acc := range bonuses {
		name := fmt.Sprintf("Bonus Pack %d", i+1)

		used := toFiniteFloat(acc["CapacityUsedPrecise"], math.NaN())
		if math.IsNaN(used) {
			used = toFiniteFloat(acc["CapacityUsed"], 0)
		}

		total := toFiniteFloat(acc["CapacitySizePrecise"], math.NaN())
		if math.IsNaN(total) {
			total = toFiniteFloat(acc["CapacitySize"], 0)
		}

		item := makeQuota(used, total, parseResetTime(acc["CycleEndTime"]), false)
		item.Recurring = &isRecurringFalse
		quotas[name] = item
	}

	plan := "CodeBuddy"

	firstPkg := accounts[0]
	if len(refills) > 0 {
		firstPkg = refills[0]
	}

	if pn, ok := firstPkg["PackageName"].(string); ok && pn != "" {
		plan = pn
	} else if spn, ok := firstPkg["SubProductName"].(string); ok && spn != "" {
		plan = spn
	}

	return &QuotaResult{
		Provider: providerId,
		Plan:     plan,
		Quotas:   quotas,
	}, nil
}

func refillCadence(acc map[string]any) string {
	startStr := parseResetTime(acc["CycleStartTime"])
	endStr := parseResetTime(acc["CycleEndTime"])

	if startStr != nil && endStr != nil {
		st, err1 := time.Parse(time.RFC3339Nano, *startStr)
		et, err2 := time.Parse(time.RFC3339Nano, *endStr)

		if err1 == nil && err2 == nil {
			days := float64(et.UnixMilli()-st.UnixMilli()) / 86400000.0
			if days <= 1.5 {
				return "Daily"
			}

			if days <= 10.0 {
				return "Weekly"
			}
		}
	}

	return "Monthly"
}
