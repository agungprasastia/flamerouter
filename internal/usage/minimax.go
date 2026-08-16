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

func queryMiniMax(ctx context.Context, opts FetchOptions, provider string) (*QuotaResult, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = opts.AccessToken
	}

	if apiKey == "" {
		return &QuotaResult{Message: "MiniMax API key not available."}, nil
	}

	urls := minimaxUsageURLs[provider]

	var lastErr string

	for i, u := range urls {
		canFallback := i < len(urls)-1

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")

		res, err := opts.HTTPClient.Do(req)
		if err != nil {
			lastErr = err.Error()

			if !canFallback {
				break
			}

			continue
		}
		defer res.Body.Close()

		rawBytes, _ := io.ReadAll(io.LimitReader(res.Body, 64*1024))

		var payload map[string]any
		_ = json.Unmarshal(rawBytes, &payload)

		baseResp, _ := payload["base_resp"].(map[string]any)
		if baseResp == nil {
			baseResp, _ = payload["baseResp"].(map[string]any)
		}

		apiStatusCode := int(toFiniteFloat(baseResp["status_code"], 0))
		if apiStatusCode == 0 {
			apiStatusCode = int(toFiniteFloat(baseResp["statusCode"], 0))
		}

		apiStatusMsg, _ := baseResp["status_msg"].(string)
		if apiStatusMsg == "" {
			apiStatusMsg, _ = baseResp["statusMsg"].(string)
		}

		combined := fmt.Sprintf("%s %s", apiStatusMsg, string(rawBytes))
		authLike := regexp.MustCompile(`(?i)(token plan|coding plan|invalid api key|invalid key|unauthorized|inactive)`).MatchString(combined)

		if res.StatusCode == 401 || res.StatusCode == 403 || apiStatusCode == 1004 || authLike {
			return &QuotaResult{Message: "MiniMax API key invalid or inactive. Use an active Token/Coding Plan key."}, nil
		}

		if res.StatusCode < 200 || res.StatusCode >= 300 {
			lastErr = fmt.Sprintf("MiniMax usage endpoint error (%d)", res.StatusCode)

			if (res.StatusCode == 404 || res.StatusCode == 405 || res.StatusCode >= 500) && canFallback {
				continue
			}

			return &QuotaResult{Message: fmt.Sprintf("MiniMax connected. %s", lastErr)}, nil
		}

		if apiStatusCode != 0 {
			msg := apiStatusMsg
			if msg == "" {
				msg = "Upstream quota API error"
			}

			return &QuotaResult{Message: fmt.Sprintf("MiniMax connected. %s", msg)}, nil
		}

		remList, _ := payload["model_remains"].([]any)
		if remList == nil {
			remList, _ = payload["modelRemains"].([]any)
		}

		if len(remList) == 0 {
			return &QuotaResult{Message: "MiniMax connected. No quota data was returned."}, nil
		}

		quotas := make(map[string]QuotaItem)
		nowMs := time.Now().UnixMilli()
		countMeansRemaining := strings.Contains(u, "/coding_plan/remains")

		for _, item := range remList {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}

			disp := formatMiniMaxQuotaName(m)
			addMiniMaxQuota(quotas, disp+" (5h)", m, "current_interval_total_count", "currentIntervalTotalCount", "current_interval_usage_count", "currentIntervalUsageCount", "current_interval_remaining_percent", "currentIntervalRemainingPercent", "remains_time", "remainsTime", "end_time", "endTime", nowMs, countMeansRemaining)
			addMiniMaxQuota(quotas, disp+" (7d)", m, "current_weekly_total_count", "currentWeeklyTotalCount", "current_weekly_usage_count", "currentWeeklyUsageCount", "current_weekly_remaining_percent", "currentWeeklyRemainingPercent", "weekly_remains_time", "weeklyRemainsTime", "weekly_end_time", "weeklyEndTime", nowMs, countMeansRemaining)
		}

		if len(quotas) == 0 {
			return &QuotaResult{Message: "MiniMax connected. Unable to extract quota usage."}, nil
		}

		return &QuotaResult{
			Provider: provider,
			Quotas:   quotas,
		}, nil
	}

	if lastErr != "" {
		return &QuotaResult{Message: fmt.Sprintf("MiniMax connected. Unable to fetch usage: %s", lastErr)}, nil
	}

	return &QuotaResult{Message: "MiniMax connected. Unable to fetch usage."}, nil
}

func formatMiniMaxQuotaName(model map[string]any) string {
	rawName, _ := model["model_name"].(string)
	if rawName == "" {
		rawName, _ = model["modelName"].(string)
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

func addMiniMaxQuota(quotas map[string]QuotaItem, key string, model map[string]any, totSnake, totCamel, cntSnake, cntCamel, pctSnake, pctCamel, remTSnake, remTCamel, endSnake, endCamel string, nowMs int64, countMeansRemaining bool) {
	tot := toFiniteFloat(getVal(model, nil, totCamel, totSnake), math.NaN())
	provPct := toFiniteFloat(getVal(model, nil, pctCamel, pctSnake), math.NaN())

	if (math.IsNaN(tot) || tot <= 0) && math.IsNaN(provPct) {
		return
	}

	cnt := toFiniteFloat(getVal(model, nil, cntCamel, cntSnake), 0)
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
		Used:                used,
		Total:               safeTot,
		Remaining:           rem,
		RemainingPercentage: remPct,
		ResetAt:             resetAt,
		Unlimited:           false,
	}
}
