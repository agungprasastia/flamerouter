package usage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
)

/* #nosec G101 */
const (
	grokCreditsURL = "https://grok.com/grok_api_v2.GrokBuildBilling/GetGrokCreditsConfig"

	grokClientIdentifier = "grok-shell"
	grokUserAgent        = "grok-shell/0.2.99"
	grokVersion          = "0.2.99"
)

func setGrokHeaders(h http.Header, token string, psd map[string]any) {
	h.Set("Authorization", "Bearer "+token)
	h.Set("Accept", "application/json")
	h.Set("User-Agent", grokUserAgent)
	h.Set("x-xai-token-auth", "xai-grok-cli")
	h.Set("x-grok-client-identifier", grokClientIdentifier)
	h.Set("x-grok-client-version", grokVersion)
	h.Set("x-grok-client-mode", "headless")

	if psd != nil {
		if em, ok := psd["email"].(string); ok && em != "" {
			h.Set("x-email", em)
		}

		if uid, ok := psd["userId"].(string); ok && uid != "" {
			h.Set("x-userid", uid)
		} else if pid, ok := psd["principalId"].(string); ok && pid != "" {
			h.Set("x-userid", pid)
		}
	}
}

func addMonthlyQuota(quotas map[string]QuotaItem, cfg, root map[string]any, periodEnd *string) {
	monthLimit := toFiniteFloat(getVal(cfg, root, "monthlyLimit", "monthly_limit"), math.NaN())
	incUsed := toFiniteFloat(getVal(cfg, root, "includedUsed", "included_used"), math.NaN())
	totUsed := toFiniteFloat(getVal(cfg, root, "totalUsed", "total_used"), math.NaN())

	if !math.IsNaN(monthLimit) && monthLimit > 0 {
		u := 0.0
		if !math.IsNaN(incUsed) {
			u = incUsed
		} else if !math.IsNaN(totUsed) {
			u = totUsed
		}

		quotas["Monthly included"] = makeQuota(u, monthLimit, periodEnd, false)
	}
}

func addOnDemandQuota(quotas map[string]QuotaItem, cfg, root map[string]any, periodEnd *string, subAccess bool) {
	onDemandCap := toFiniteFloat(getVal(cfg, root, "onDemandCap", "on_demand_cap"), math.NaN())
	onDemandUsed := toFiniteFloat(getVal(cfg, root, "onDemandUsed", "on_demand_used"), math.NaN())

	if !math.IsNaN(onDemandCap) && onDemandCap > 0 {
		u := 0.0
		if !math.IsNaN(onDemandUsed) {
			u = math.Max(0, onDemandUsed)
		}

		quotas["On-demand"] = makeQuota(u, onDemandCap, periodEnd, false)
	} else if !subAccess && !math.IsNaN(onDemandCap) && onDemandCap == 0 && !math.IsNaN(onDemandUsed) {
		quotas["On-demand"] = QuotaItem{
			ResetAt:             periodEnd,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                1,
			Total:               1,
			Remaining:           0,
			RemainingPercentage: 0,
			Unlimited:           false,
		}
	}
}

func addPrepaidAndCredits(quotas map[string]QuotaItem, cfg, root map[string]any, periodEnd *string) {
	prepaid := toFiniteFloat(getVal(cfg, root, "prepaidBalance", "prepaid_balance"), math.NaN())
	if !math.IsNaN(prepaid) && prepaid > 0 {
		quotas["Prepaid"] = QuotaItem{
			ResetAt:             nil,
			Recurring:           nil,
			DisplayName:         "",
			Unit:                "",
			Used:                0,
			Total:               prepaid,
			Remaining:           prepaid,
			RemainingPercentage: 100,
			Unlimited:           false,
		}
	}

	creditUsagePct := toFiniteFloat(getVal(cfg, root, "creditUsagePercent", "credit_usage_percent"), math.NaN())
	if !math.IsNaN(creditUsagePct) && creditUsagePct >= 0 {
		u := math.Max(0, math.Min(100, creditUsagePct))
		quotas["Weekly SuperGrok"] = makeQuota(u, 100, periodEnd, false)
	}
}

func parseGrokCliBilling(billing, user map[string]any) *QuotaResult {
	root := billing
	cfg := root

	if c, ok := root["config"].(map[string]any); ok && c != nil {
		cfg = c
	}

	periodEnd := getGrokPeriodEnd(cfg, root)
	tier := getGrokSubscriptionTier(user, cfg)
	subAccess := tier != "" && !strings.EqualFold(tier, "free") && !strings.EqualFold(tier, "none") && !strings.EqualFold(tier, "null")

	quotas := make(map[string]QuotaItem)
	addMonthlyQuota(quotas, cfg, root, periodEnd)
	addOnDemandQuota(quotas, cfg, root, periodEnd, subAccess)
	addPrepaidAndCredits(quotas, cfg, root, periodEnd)

	plan := resolveGrokPlan(user, cfg)

	return &QuotaResult{
		Provider:           "grok-cli",
		Plan:               plan,
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      0,
		LimitReached:       nil,
		ReviewLimitReached: nil,
		IsQuotaExceeded:    nil,
		ResetCredits:       nil,
		ResetsAt:           periodEnd,
		Message:            "",
		Quotas:             quotas,
		Details: map[string]any{
			"subscriptionAccess": subAccess,
		},
	}
}

func getMapVal(m map[string]any, camel, snake string) any {
	if m == nil {
		return nil
	}

	if v, ok := m[camel]; ok && v != nil {
		return v
	}

	if v, ok := m[snake]; ok && v != nil {
		return v
	}

	return nil
}

func getVal(cfg, root map[string]any, camel, snake string) any {
	if v := getMapVal(cfg, camel, snake); v != nil {
		return v
	}

	return getMapVal(root, camel, snake)
}

func getGrokPeriodEnd(cfg, root map[string]any) *string {
	keys := []string{"billingPeriodEnd", "billing_period_end", "resetAt", "resetsAt", "periodEnd"}
	for _, k := range keys {
		if v, ok := cfg[k]; ok {
			if res := parseResetTime(v); res != nil {
				return res
			}
		}

		if v, ok := root[k]; ok {
			if res := parseResetTime(v); res != nil {
				return res
			}
		}
	}

	if cp, ok := cfg["currentPeriod"].(map[string]any); ok {
		if res := parseResetTime(cp["end"]); res != nil {
			return res
		}
	}

	return nil
}

func getGrokSubscriptionTier(user, cfg map[string]any) string {
	for _, m := range []map[string]any{user, cfg} {
		if m == nil {
			continue
		}

		for _, k := range []string{"subscriptionTier", "subscription_tier"} {
			if s, ok := m[k].(string); ok && s != "" {
				return strings.TrimSpace(s)
			}
		}

		if sub, ok := m["subscription"].(map[string]any); ok && sub != nil {
			if s, ok := sub["tier"].(string); ok && s != "" {
				return strings.TrimSpace(s)
			}
		}
	}

	return ""
}

func resolveGrokPlan(user, cfg map[string]any) string {
	tier := getGrokSubscriptionTier(user, cfg)
	if tier != "" {
		tier = strings.ReplaceAll(tier, "_", " ")
		tier = strings.ReplaceAll(tier, "-", " ")

		return titleCase(tier)
	}

	if user != nil {
		if codeAcc, ok := user["hasGrokCodeAccess"].(bool); ok && codeAcc {
			return "Grok Code"
		}
	}

	if cfg != nil {
		if unif, ok := cfg["isUnifiedBillingUser"].(bool); ok && unif {
			return "Grok Build"
		}
	}

	return "Grok Build"
}

var grokTierNames = map[int]string{
	0: "Free",
	1: "SuperGrok",
	2: "X Basic",
	3: "X Premium",
	4: "X Premium Plus",
	5: "SuperGrok Heavy",
	6: "SuperGrok Lite",
}

func planFromGrokAccessToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	var data struct {
		Tier int `json:"tier"`
	}

	if err := json.Unmarshal(payloadBytes, &data); err != nil {
		return ""
	}

	return grokTierNames[data.Tier]
}

func fetchGrokGrpcCredits(ctx context.Context, opts FetchOptions) (float64, *string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, grokCreditsURL, bytes.NewReader([]byte{0, 0, 0, 0, 0}))
	if err != nil {
		return 0, nil, false
	}

	req.Header.Set("Authorization", "Bearer "+opts.AccessToken)
	req.Header.Set("Content-Type", "application/grpc-web+proto")
	req.Header.Set("X-Grpc-Web", "1")
	req.Header.Set("Accept", "application/grpc-web+proto")

	res, err := doHTTP(opts.HTTPClient, req)
	if err != nil {
		return 0, nil, false
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return 0, nil, false
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(res.Body, 64*1024))
	if err != nil || len(bodyBytes) == 0 {
		return 0, nil, false
	}

	pct, resetAt, ok := DecodeGrokCreditsFrame(bodyBytes)
	if !ok {
		return 0, nil, false
	}

	return math.Round(pct), resetAt, true
}
