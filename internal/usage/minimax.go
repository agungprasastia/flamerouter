package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var minimaxUsageURLs = map[string][]string{
	"minimax": {
		"https://api.minimax.chat/v1/coding_plan/remains",
		"https://api.minimax.chat/v1/token_plan/remains",
	},
	"minimax-cn": {
		"https://api.minimaxi.chat/v1/coding_plan/remains",
		"https://api.minimaxi.chat/v1/token_plan/remains",
	},
}

var authPattern = regexp.MustCompile(`(?i)(token plan|coding plan|invalid api key|invalid key|unauthorized|inactive)`)

func init() {
	RegisterQuotaHandler("minimax", fetchMiniMaxUsage)
	RegisterQuotaHandler("minimax-cn", fetchMiniMaxCnUsage)
}

func fetchMiniMaxUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return queryMiniMax(ctx, opts, "minimax")
}

func fetchMiniMaxCnUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	return queryMiniMax(ctx, opts, "minimax-cn")
}

type miniMaxStatus struct {
	msg  string
	code int
}

func extractMiniMaxBaseStatus(payload map[string]any) miniMaxStatus {
	var baseResp map[string]any
	if val, ok := payload["base_resp"].(map[string]any); ok {
		baseResp = val
	} else if val, ok := payload["baseResp"].(map[string]any); ok {
		baseResp = val
	}

	apiStatusCode := int(toFiniteFloat(baseResp["status_code"], 0))
	if apiStatusCode == 0 {
		apiStatusCode = int(toFiniteFloat(baseResp["statusCode"], 0))
	}

	var apiStatusMsg string
	if val, ok := baseResp["status_msg"].(string); ok {
		apiStatusMsg = val
	} else if val, ok := baseResp["statusMsg"].(string); ok {
		apiStatusMsg = val
	}

	return miniMaxStatus{code: apiStatusCode, msg: apiStatusMsg}
}

func parseMiniMaxModelRemains(payload map[string]any, isCodingPlan bool, nowMs int64) map[string]QuotaItem {
	var remList []any
	if val, ok := payload["model_remains"].([]any); ok {
		remList = val
	} else if val, ok := payload["modelRemains"].([]any); ok {
		remList = val
	}

	if len(remList) == 0 {
		return nil
	}

	quotas := make(map[string]QuotaItem)

	for _, item := range remList {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		disp := formatMiniMaxQuotaName(m)
		addMiniMaxIntervalQuota(quotas, disp+" (5h)", m, nowMs, isCodingPlan)
		addMiniMaxWeeklyQuota(quotas, disp+" (7d)", m, nowMs, isCodingPlan)
	}

	return quotas
}

func makeMiniMaxResult(provider, msg string, quotas map[string]QuotaItem) *QuotaResult {
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
		Message:            msg,
		Details:            nil,
		Quotas:             quotas,
	}
}

func queryMiniMax(ctx context.Context, opts FetchOptions, provider string) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return makeMiniMaxResult(provider, "MiniMax API key not available.", nil), nil
	}

	urls := minimaxUsageURLs[provider]

	var lastErr string

	for i, u := range urls {
		qRes, cont, err := executeMiniMaxRequest(ctx, opts, apiKey, u, i < len(urls)-1, provider)
		if err != nil {
			lastErr = err.Error()

			if i >= len(urls)-1 {
				break
			}

			continue
		}

		if cont {
			if qRes != nil {
				lastErr = qRes.Message
			}

			continue
		}

		return qRes, nil
	}

	msg := "MiniMax connected. Unable to fetch usage."
	if lastErr != "" {
		msg = fmt.Sprintf("MiniMax connected. Unable to fetch usage: %s", lastErr)
	}

	return makeMiniMaxResult(provider, msg, nil), nil
}

func parseMiniMaxHTTPResponse(res *http.Response) ([]byte, map[string]any) {
	rawBytes, readErr := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if readErr != nil {
		rawBytes = nil
	}

	var payload map[string]any
	if unmarshalErr := json.Unmarshal(rawBytes, &payload); unmarshalErr != nil {
		payload = nil
	}

	return rawBytes, payload
}

func isMiniMaxAuthError(res *http.Response, rawBytes []byte, st miniMaxStatus) bool {
	combined := fmt.Sprintf("%s %s", st.msg, string(rawBytes))
	authLike := authPattern.MatchString(combined)

	return res.StatusCode == 401 || res.StatusCode == 403 || st.code == 1004 || authLike
}

func handleMiniMaxHTTPStatusError(res *http.Response, provider string, canFallback bool) (*QuotaResult, bool, bool) {
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil, false, false
	}

	errStr := fmt.Sprintf("MiniMax usage endpoint error (%d)", res.StatusCode)
	if (res.StatusCode == 404 || res.StatusCode == 405 || res.StatusCode >= 500) && canFallback {
		return makeMiniMaxResult(provider, errStr, nil), true, true
	}

	return makeMiniMaxResult(provider, fmt.Sprintf("MiniMax connected. %s", errStr), nil), false, true
}

func checkMiniMaxErrors(res *http.Response, rawBytes []byte, payload map[string]any, provider string, canFallback bool) (*QuotaResult, bool, bool) {
	st := extractMiniMaxBaseStatus(payload)
	if isMiniMaxAuthError(res, rawBytes, st) {
		return makeMiniMaxResult(provider, "MiniMax API key invalid or inactive. Use an active Token/Coding Plan key.", nil), false, true
	}

	if errRes, cont, matched := handleMiniMaxHTTPStatusError(res, provider, canFallback); matched {
		return errRes, cont, true
	}

	if st.code != 0 {
		msg := st.msg
		if msg == "" {
			msg = "Upstream quota API error"
		}

		return makeMiniMaxResult(provider, fmt.Sprintf("MiniMax connected. %s", msg), nil), false, true
	}

	return nil, false, false
}

func executeMiniMaxRequest(ctx context.Context, opts FetchOptions, apiKey, u string, canFallback bool, provider string) (*QuotaResult, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return nil, false, err
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	rawBytes, payload := parseMiniMaxHTTPResponse(res)

	if errResult, cont, matched := checkMiniMaxErrors(res, rawBytes, payload, provider, canFallback); matched {
		return errResult, cont, nil
	}

	nowMs := time.Now().UnixMilli()
	countMeansRemaining := strings.Contains(u, "/coding_plan/remains")
	quotas := parseMiniMaxModelRemains(payload, countMeansRemaining, nowMs)

	if len(quotas) == 0 {
		return makeMiniMaxResult(provider, "MiniMax connected. No quota data was returned.", nil), false, nil
	}

	return makeMiniMaxResult(provider, "", quotas), false, nil
}

func formatMiniMaxQuotaName(model map[string]any) string {
	var rawName string
	if val, ok := model["model_name"].(string); ok {
		rawName = val
	} else if val, ok := model["modelName"].(string); ok {
		rawName = val
	}

	rawName = strings.TrimSpace(rawName)
	if rawName == "" {
		return "MiniMax"
	}

	if rawName == "MiniMax-M*" || rawName == "general" {
		return "M-series"
	}

	res := strings.ReplaceAll(rawName, "_", " ")
	res = strings.ReplaceAll(res, "-", " ")

	return titleCase(res)
}

func addMiniMaxIntervalQuota(quotas map[string]QuotaItem, key string, model map[string]any, nowMs int64, countMeansRemaining bool) {
	addMiniMaxQuota(quotas, key, model, "current_interval_total_count", "currentIntervalTotalCount", "current_interval_usage_count", "currentIntervalUsageCount", "current_interval_remaining_percent", "currentIntervalRemainingPercent", "remains_time", "remainsTime", "end_time", "endTime", nowMs, countMeansRemaining)
}

func addMiniMaxWeeklyQuota(quotas map[string]QuotaItem, key string, model map[string]any, nowMs int64, countMeansRemaining bool) {
	addMiniMaxQuota(quotas, key, model, "current_weekly_total_count", "currentWeeklyTotalCount", "current_weekly_usage_count", "currentWeeklyUsageCount", "current_weekly_remaining_percent", "currentWeeklyRemainingPercent", "weekly_remains_time", "weeklyRemainsTime", "weekly_end_time", "weeklyEndTime", nowMs, countMeansRemaining)
}

func calculateMiniMaxEffValues(tot, provPct, cnt float64, countMeansRemaining bool) (float64, float64) {
	effTot := tot
	effCnt := cnt

	if math.IsNaN(tot) || tot <= 0 {
		effTot = 100
		if countMeansRemaining {
			effCnt = math.Round(effTot * (provPct / 100.0))
		} else {
			effCnt = math.Round(effTot * (1.0 - provPct/100.0))
		}
	}

	return effTot, effCnt
}

func addMiniMaxQuota(quotas map[string]QuotaItem, key string, model map[string]any, totSnake, totCamel, cntSnake, cntCamel, pctSnake, pctCamel, remTSnake, remTCamel, endSnake, endCamel string, nowMs int64, countMeansRemaining bool) {
	tot := toFiniteFloat(getVal(model, nil, totCamel, totSnake), math.NaN())
	provPct := toFiniteFloat(getVal(model, nil, pctCamel, pctSnake), math.NaN())

	if (math.IsNaN(tot) || tot <= 0) && math.IsNaN(provPct) {
		return
	}

	cnt := toFiniteFloat(getVal(model, nil, cntCamel, cntSnake), 0)
	effTot, effCnt := calculateMiniMaxEffValues(tot, provPct, cnt, countMeansRemaining)

	safeTot := math.Max(0, effTot)

	var used float64
	if countMeansRemaining {
		used = math.Max(0, safeTot-effCnt)
	} else {
		used = math.Min(math.Max(0, effCnt), safeTot)
	}

	rem := math.Max(0, safeTot-used)

	remPct := 0.0
	if !math.IsNaN(provPct) {
		remPct = math.Max(0, math.Min(100, provPct))
	} else if safeTot > 0 {
		remPct = math.Max(0, math.Min(100, (rem/safeTot)*100.0))
	}

	var resetAt *string

	remainsMs := int64(toFiniteFloat(getVal(model, nil, remTCamel, remTSnake), 0))
	if remainsMs > 0 {
		iso := time.UnixMilli(nowMs + remainsMs).UTC().Format(time.RFC3339Nano)
		resetAt = &iso
	} else {
		resetAt = parseResetTime(getVal(model, nil, endCamel, endSnake))
	}

	quotas[key] = QuotaItem{
		ResetAt:             resetAt,
		Recurring:           nil,
		DisplayName:         "",
		Unit:                "",
		Used:                used,
		Total:               safeTot,
		Remaining:           rem,
		RemainingPercentage: remPct,
		Unlimited:           false,
	}
}
