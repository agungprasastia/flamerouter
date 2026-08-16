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
			Used:                1,
			Total:               1,
			RemainingPercentage: 0,
			ResetAt:             periodEnd,
			Unlimited:           false,
		}
	}

	prepaid := toFiniteFloat(getVal(cfg, root, "prepaidBalance", "prepaid_balance"), math.NaN())
	if !math.IsNaN(prepaid) && prepaid > 0 {
		quotas["Prepaid"] = QuotaItem{
			Used:                0,
			Total:               prepaid,
			RemainingPercentage: 100,
			ResetAt:             nil,
			Unlimited:           false,
		}
	}

	creditUsagePct := toFiniteFloat(getVal(cfg, root, "creditUsagePercent", "credit_usage_percent"), math.NaN())
	if !math.IsNaN(creditUsagePct) && creditUsagePct >= 0 {
		u := math.Max(0, math.Min(100, creditUsagePct))
		quotas["Weekly SuperGrok"] = makeQuota(u, 100, periodEnd, false)
	}

	plan := resolveGrokPlan(user, cfg)

	return &QuotaResult{
		Provider: "grok-cli",
		Plan:     plan,
		Quotas:   quotas,
		ResetsAt: periodEnd,
		Details: map[string]any{
			"subscriptionAccess": subAccess,
		},
	}
}

func getVal(cfg, root map[string]any, camel, snake string) any {
	if cfg != nil {
		if v, ok := cfg[camel]; ok && v != nil {
			return v
		}

		if v, ok := cfg[snake]; ok && v != nil {
			return v
		}
	}

	if root != nil {
		if v, ok := root[camel]; ok && v != nil {
			return v
		}

		if v, ok := root[snake]; ok && v != nil {
			return v
		}
	}

	return nil
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

	switch data.Tier {
	case 0:
		return "Free"
	case 1:
		return "SuperGrok"
	case 2:
		return "X Basic"
	case 3:
		return "X Premium"
	case 4:
		return "X Premium Plus"
	case 5:
		return "SuperGrok Heavy"
	case 6:
		return "SuperGrok Lite"
	default:
		return ""
	}
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

	res, err := opts.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, false
	}

	defer res.Body.Close()

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
