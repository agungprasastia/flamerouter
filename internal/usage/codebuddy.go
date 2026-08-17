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

func codebuddyCycleEndMs(acc map[string]any) float64 {
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

func isCodebuddyRefill(acc map[string]any) bool {
	const refillGapMs = 2.0 * 24.0 * 60.0 * 60.0 * 1000.0

	ce := codebuddyCycleEndMs(acc)
	de := toFiniteFloat(acc["DeductionEndTime"], math.NaN())

	return !math.IsInf(ce, 0) && !math.IsNaN(de) && (de-ce > refillGapMs)
}

func splitCodebuddyAccounts(accounts []map[string]any) ([]map[string]any, []map[string]any) {
	var refills []map[string]any

	var bonuses []map[string]any

	for _, acc := range accounts {
		if isCodebuddyRefill(acc) {
			refills = append(refills, acc)
		} else {
			bonuses = append(bonuses, acc)
		}
	}

	sort.Slice(refills, func(i, j int) bool {
		return codebuddyCycleEndMs(refills[i]) < codebuddyCycleEndMs(refills[j])
	})

	sort.Slice(bonuses, func(i, j int) bool {
		return codebuddyCycleEndMs(bonuses[i]) < codebuddyCycleEndMs(bonuses[j])
	})

	return refills, bonuses
}

func buildCodebuddyQuotas(refills, bonuses []map[string]any) map[string]QuotaItem {
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

	return quotas
}

func determineCodebuddyPlan(accounts, refills []map[string]any) string {
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

	return plan
}

type codebuddyJSONBody struct {
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

func checkCodebuddyResponseStatus(res *http.Response, providerID string) *QuotaResult {
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{
			Provider:           providerID,
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
			Message:            "CodeBuddy CN credential invalid or expired.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           providerID,
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
			Message:            fmt.Sprintf("CodeBuddy CN quota API error (%d).", res.StatusCode),
			Details:            nil,
			Quotas:             nil,
		}
	}

	return nil
}

func parseCodebuddySuccessBody(jsonBody codebuddyJSONBody, providerID string) *QuotaResult {
	accounts := jsonBody.Data.Response.Data.Accounts
	if len(accounts) == 0 {
		return &QuotaResult{
			Provider:           providerID,
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
			Message:            "CodeBuddy CN connected. No credit package found.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	refills, bonuses := splitCodebuddyAccounts(accounts)
	quotas := buildCodebuddyQuotas(refills, bonuses)
	plan := determineCodebuddyPlan(accounts, refills)

	return &QuotaResult{
		Provider:           providerID,
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

func parseCodebuddyResponse(res *http.Response, providerID string) *QuotaResult {
	var jsonBody codebuddyJSONBody

	if err := json.NewDecoder(res.Body).Decode(&jsonBody); err != nil {
		return &QuotaResult{
			Provider:           providerID,
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
			Message:            "CodeBuddy CN quota response was not JSON.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	if jsonBody.Code != 0 {
		msg := jsonBody.Msg
		if msg == "" {
			msg = "unknown"
		}

		return &QuotaResult{
			Provider:           providerID,
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
			Message:            fmt.Sprintf("CodeBuddy CN quota error: %s", msg),
			Details:            nil,
			Quotas:             nil,
		}
	}

	return parseCodebuddySuccessBody(jsonBody, providerID)
}

func executeCodebuddyRequest(ctx context.Context, opts FetchOptions, providerID, quotaURL, token string) (*http.Response, *QuotaResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, quotaURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, &QuotaResult{
			Provider:           providerID,
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
			Message:            fmt.Sprintf("CodeBuddy (%s) error: %v", providerID, err),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return res, nil, nil
}

func queryCodebuddy(ctx context.Context, opts FetchOptions, providerID, quotaURL string) (*QuotaResult, error) {
	token := opts.AccessToken
	if token == "" {
		token = opts.APIKey
	}

	if token == "" {
		return &QuotaResult{
			Provider:           providerID,
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
			Message:            fmt.Sprintf("CodeBuddy (%s) credential not available.", providerID),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	res, errRes, err := executeCodebuddyRequest(ctx, opts, providerID, quotaURL, token)
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

	if statusRes := checkCodebuddyResponseStatus(res, providerID); statusRes != nil {
		return statusRes, nil
	}

	return parseCodebuddyResponse(res, providerID), nil
}

func refillCadence(acc map[string]any) string {
	startStr := parseResetTime(acc["CycleStartTime"])
	endStr := parseResetTime(acc["CycleEndTime"])

	if startStr == nil || endStr == nil {
		return "Monthly"
	}

	st, err1 := time.Parse(time.RFC3339Nano, *startStr)
	et, err2 := time.Parse(time.RFC3339Nano, *endStr)

	if err1 != nil || err2 != nil {
		return "Monthly"
	}

	days := float64(et.UnixMilli()-st.UnixMilli()) / 86400000.0
	if days <= 1.5 {
		return "Daily"
	}

	if days <= 10.0 {
		return "Weekly"
	}

	return "Monthly"
}
