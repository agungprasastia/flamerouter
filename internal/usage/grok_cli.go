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

func parseGrokErrorMessage(res *http.Response) string {
	errBytes, err := io.ReadAll(io.LimitReader(res.Body, 512))
	if err != nil {
		return fmt.Sprintf("Grok CLI billing API error (%d)", res.StatusCode)
	}

	trimmed := strings.TrimSpace(string(errBytes))
	if len(trimmed) > 200 {
		trimmed = trimmed[:200]
	}

	if trimmed != "" {
		return fmt.Sprintf("Grok CLI billing API error (%d): %s", res.StatusCode, trimmed)
	}

	return fmt.Sprintf("Grok CLI billing API error (%d)", res.StatusCode)
}

func checkGrokBillingStatus(res *http.Response) *QuotaResult {
	if res.StatusCode == 401 || res.StatusCode == 403 {
		return &QuotaResult{
			Provider:           "grok-cli",
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
			Message:            "Grok CLI authentication expired. Please re-authorize.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &QuotaResult{
			Provider:           "grok-cli",
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
			Message:            parseGrokErrorMessage(res),
			Details:            nil,
			Quotas:             nil,
		}
	}

	return nil
}

func fetchGrokUserData(ctx context.Context, opts FetchOptions) map[string]any {
	reqUser, err := http.NewRequestWithContext(ctx, http.MethodGet, grokUserURL, nil)
	if err != nil {
		return nil
	}

	setGrokHeaders(reqUser.Header, opts.AccessToken, opts.ProviderSpecificData)

	resUser, errUser := doHTTP(opts.HTTPClient, reqUser)
	if errUser != nil {
		return nil
	}

	defer func() {
		if closeErr := resUser.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	if resUser.StatusCode < 200 || resUser.StatusCode >= 300 {
		return nil
	}

	var userData map[string]any
	if decodeErr := json.NewDecoder(resUser.Body).Decode(&userData); decodeErr != nil {
		return nil
	}

	return userData
}

func executeGrokBillingRequest(ctx context.Context, opts FetchOptions) (*http.Response, *QuotaResult, error) {
	reqBilling, err := http.NewRequestWithContext(ctx, http.MethodGet, grokBillingURL, nil)
	if err != nil {
		return nil, nil, err
	}

	setGrokHeaders(reqBilling.Header, opts.AccessToken, opts.ProviderSpecificData)

	resBilling, err := doHTTP(opts.HTTPClient, reqBilling)
	if err != nil {
		return nil, &QuotaResult{
			Provider:           "grok-cli",
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
			Message:            fmt.Sprintf("Grok CLI usage error: %v", err),
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	return resBilling, nil, nil
}

func finalizeGrokCliResult(ctx context.Context, opts FetchOptions, parsed *QuotaResult) *QuotaResult {
	if jwtPlan := planFromGrokAccessToken(opts.AccessToken); jwtPlan != "" {
		parsed.Plan = jwtPlan
	}

	if len(parsed.Quotas) > 0 {
		return parsed
	}

	grpcPct, grpcReset, grpcOK := fetchGrokGrpcCredits(ctx, opts)
	if grpcOK {
		parsed.Quotas = map[string]QuotaItem{
			"Weekly SuperGrok": makeQuota(grpcPct, 100, grpcReset, false),
		}

		return parsed
	}

	if parsed.Details != nil && parsed.Details["subscriptionAccess"] == true {
		parsed.Message = "Subscription access is active; Grok does not expose a numeric included quota."
	} else {
		parsed.Message = "Grok Build connected, but no credit allotment was returned. Free promo may be exhausted."
	}

	return parsed
}

func parseGrokCliResponse(ctx context.Context, opts FetchOptions, resBilling *http.Response) *QuotaResult {
	var billingData map[string]any
	if decodeErr := json.NewDecoder(resBilling.Body).Decode(&billingData); decodeErr != nil {
		return &QuotaResult{
			Provider:           "grok-cli",
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
			Message:            "Grok CLI billing response was not JSON.",
			Details:            nil,
			Quotas:             nil,
		}
	}

	userData := fetchGrokUserData(ctx, opts)
	parsed := parseGrokCliBilling(billingData, userData)

	return finalizeGrokCliResult(ctx, opts, parsed)
}

func fetchGrokCliUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{
			Provider:           "grok-cli",
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
			Message:            "Grok CLI access token not available.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	resBilling, errRes, err := executeGrokBillingRequest(ctx, opts)
	if err != nil {
		return nil, err
	}

	if errRes != nil || resBilling == nil {
		return errRes, nil
	}

	defer func() {
		if resBilling != nil && resBilling.Body != nil {
			if closeErr := resBilling.Body.Close(); closeErr != nil {
				_ = closeErr
			}
		}
	}()

	if statusRes := checkGrokBillingStatus(resBilling); statusRes != nil {
		return statusRes, nil
	}

	return parseGrokCliResponse(ctx, opts, resBilling), nil
}
