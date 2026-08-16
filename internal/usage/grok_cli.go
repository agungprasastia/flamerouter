package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	grokBillingURL = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	grokUserURL    = "https://cli-chat-proxy.grok.com/v1/user?include=subscription"
)

func init() {
	RegisterQuotaHandler("grok-cli", fetchGrokCliUsage)
}

func fetchGrokCliUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{Message: "Grok CLI access token not available."}, nil
	}

	reqBilling, err := http.NewRequestWithContext(ctx, http.MethodGet, grokBillingURL, nil)
	if err != nil {
		return nil, err
	}

	setGrokHeaders(reqBilling.Header, opts.AccessToken, opts.ProviderSpecificData)

	resBilling, err := opts.HTTPClient.Do(reqBilling)
	if err != nil {
		return &QuotaResult{Message: fmt.Sprintf("Grok CLI usage error: %v", err)}, nil
	}
	defer resBilling.Body.Close()

	if resBilling.StatusCode == 401 || resBilling.StatusCode == 403 {
		return &QuotaResult{Message: "Grok CLI authentication expired. Please re-authorize."}, nil
	}

	if resBilling.StatusCode < 200 || resBilling.StatusCode >= 300 {
		errBytes, _ := io.ReadAll(io.LimitReader(resBilling.Body, 512))

		trimmed := strings.TrimSpace(string(errBytes))
		if len(trimmed) > 200 {
			trimmed = trimmed[:200]
		}

		if trimmed != "" {
			return &QuotaResult{Message: fmt.Sprintf("Grok CLI billing API error (%d): %s", resBilling.StatusCode, trimmed)}, nil
		}

		return &QuotaResult{Message: fmt.Sprintf("Grok CLI billing API error (%d)", resBilling.StatusCode)}, nil
	}

	var billingData map[string]any
	if err := json.NewDecoder(resBilling.Body).Decode(&billingData); err != nil {
		return &QuotaResult{Message: "Grok CLI billing response was not JSON."}, nil
	}

	var userData map[string]any

	reqUser, err := http.NewRequestWithContext(ctx, http.MethodGet, grokUserURL, nil)
	if err == nil {
		setGrokHeaders(reqUser.Header, opts.AccessToken, opts.ProviderSpecificData)

		if resUser, err := opts.HTTPClient.Do(reqUser); err == nil {
			defer resUser.Body.Close()

			if resUser.StatusCode >= 200 && resUser.StatusCode < 300 {
				_ = json.NewDecoder(resUser.Body).Decode(&userData)
			}
		}
	}

	parsed := parseGrokCliBilling(billingData, userData)
	if jwtPlan := planFromGrokAccessToken(opts.AccessToken); jwtPlan != "" {
		parsed.Plan = jwtPlan
	}

	if len(parsed.Quotas) == 0 {
		grpcPct, grpcReset, grpcOK := fetchGrokGrpcCredits(ctx, opts)
		if grpcOK {
			parsed.Quotas = map[string]QuotaItem{
				"Weekly SuperGrok": makeQuota(grpcPct, 100, grpcReset, false),
			}

			return parsed, nil
		}

		if parsed.Details != nil && parsed.Details["subscriptionAccess"] == true {
			parsed.Message = "Subscription access is active; Grok does not expose a numeric included quota."
		} else {
			parsed.Message = "Grok Build connected, but no credit allotment was returned. Free promo may be exhausted."
		}
	}

	return parsed, nil
}
