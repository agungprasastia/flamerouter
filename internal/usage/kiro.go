package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	kiroCwHost     = "https://codewhisperer.us-east-1.amazonaws.com"
	kiroLimitsPath = "/getUsageLimits"
	kiroQHost      = "https://q.us-east-1.amazonaws.com"
)

func init() {
	RegisterQuotaHandler("kiro", fetchKiroUsage)
}

type kiroAttempt struct {
	exec func() (*http.Response, error)
	name string
}

func makeCwGetAttempt(ctx context.Context, opts FetchOptions, isAPIKey, isExternalIDP bool) kiroAttempt {
	params := url.Values{
		"isEmailRequired": {"true"},
		"origin":          {"AI_EDITOR"},
		"resourceType":    {"AGENTIC_REQUEST"},
	}
	cwGetURL := fmt.Sprintf("%s%s?%s", kiroCwHost, kiroLimitsPath, params.Encode())

	return kiroAttempt{
		name: "codewhisperer-get",
		exec: func() (*http.Response, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, cwGetURL, nil)
			if err != nil {
				return nil, err
			}

			setKiroHeaders(req.Header, opts.AccessToken, isAPIKey, isExternalIDP)

			return doHTTP(opts.HTTPClient, req)
		},
	}
}

func makeCwPostAttempt(ctx context.Context, opts FetchOptions, isAPIKey, isExternalIDP bool, profileArn string) kiroAttempt {
	return kiroAttempt{
		name: "codewhisperer-post",
		exec: func() (*http.Response, error) {
			bodyMap := map[string]any{
				"origin":       "AI_EDITOR",
				"resourceType": "AGENTIC_REQUEST",
			}
			if profileArn != "" {
				bodyMap["profileArn"] = profileArn
			}

			bodyBytes, err := json.Marshal(bodyMap)
			if err != nil {
				bodyBytes = []byte("{}")
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, kiroCwHost, bytes.NewReader(bodyBytes))
			if err != nil {
				return nil, err
			}

			setKiroHeaders(req.Header, opts.AccessToken, isAPIKey, isExternalIDP)
			req.Header.Set("Content-Type", "application/x-amz-json-1.0")
			req.Header.Set("x-amz-target", "AmazonCodeWhispererService.GetUsageLimits")

			return doHTTP(opts.HTTPClient, req)
		},
	}
}

func makeQGetAttempt(ctx context.Context, opts FetchOptions, isAPIKey, isExternalIDP bool, profileArn string) kiroAttempt {
	return kiroAttempt{
		name: "q-get",
		exec: func() (*http.Response, error) {
			qParams := url.Values{
				"origin":       {"AI_EDITOR"},
				"resourceType": {"AGENTIC_REQUEST"},
			}
			if profileArn != "" {
				qParams.Set("profileArn", profileArn)
			}

			qGetURL := fmt.Sprintf("%s%s?%s", kiroQHost, kiroLimitsPath, qParams.Encode())

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, qGetURL, nil)
			if err != nil {
				return nil, err
			}

			setKiroHeaders(req.Header, opts.AccessToken, isAPIKey, isExternalIDP)

			return doHTTP(opts.HTTPClient, req)
		},
	}
}

func buildKiroAttempts(ctx context.Context, opts FetchOptions, isAPIKey, isExternalIDP bool, profileArn string) []kiroAttempt {
	return []kiroAttempt{
		makeCwGetAttempt(ctx, opts, isAPIKey, isExternalIDP),
		makeCwPostAttempt(ctx, opts, isAPIKey, isExternalIDP, profileArn),
		makeQGetAttempt(ctx, opts, isAPIKey, isExternalIDP, profileArn),
	}
}

func handleKiroAuthError(authMethod string) *QuotaResult {
	if authMethod == "idc" {
		return &QuotaResult{
			Provider:           "kiro",
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
			Message:            "Kiro quota API is unavailable for the current AWS IAM Identity Center session. Chat may still work. If this persists after renewing your session, reconnect Kiro.",
			Details:            nil,
			Quotas:             map[string]QuotaItem{},
		}
	}

	return &QuotaResult{
		Provider:           "kiro",
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
		Message:            "Kiro quota API authentication expired. Chat may still work.",
		Details:            nil,
		Quotas:             map[string]QuotaItem{},
	}
}

func executeKiroAttempt(att kiroAttempt, authMethod string) (*QuotaResult, string, bool) {
	res, err := att.exec()
	if err != nil {
		return nil, err.Error(), false
	}

	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	if res.StatusCode == 401 || res.StatusCode == 403 {
		return handleKiroAuthError(authMethod), "", true
	}

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errBytes, readErr := io.ReadAll(io.LimitReader(res.Body, 512))
		if readErr != nil {
			errBytes = nil
		}

		return nil, fmt.Sprintf("%s: %d %s", att.name, res.StatusCode, strings.TrimSpace(string(errBytes))), false
	}

	var data map[string]any
	if decodeErr := json.NewDecoder(res.Body).Decode(&data); decodeErr == nil {
		return parseKiroQuotaData(data), "", true
	}

	return nil, "decode error", false
}

func extractKiroAuthDetails(data map[string]any) (string, bool, bool, string) {
	var authMethod string
	if val, ok := data["authMethod"].(string); ok {
		authMethod = val
	}

	if authMethod == "" {
		authMethod = "builder-id"
	}

	isAPIKey := authMethod == "api_key"
	isExternalIDP := authMethod == "external_idp"

	var profileArn string
	if val, ok := data["profileArn"].(string); ok {
		profileArn = val
	}

	return authMethod, isAPIKey, isExternalIDP, profileArn
}

func fetchKiroUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{
			Provider:           "kiro",
			Plan:               "Kiro",
			Limit:              0,
			Used:               0,
			Remaining:          0,
			TotalUsagePct:      0,
			LimitReached:       nil,
			ReviewLimitReached: nil,
			IsQuotaExceeded:    nil,
			ResetCredits:       nil,
			ResetsAt:           nil,
			Message:            "Kiro access token not available.",
			Details:            nil,
			Quotas:             nil,
		}, nil
	}

	authMethod, isAPIKey, isExternalIDP, profileArn := extractKiroAuthDetails(opts.ProviderSpecificData)
	attempts := buildKiroAttempts(ctx, opts, isAPIKey, isExternalIDP, profileArn)

	var lastErr string

	for _, att := range attempts {
		result, errMsg, done := executeKiroAttempt(att, authMethod)
		if done {
			return result, nil
		}

		if errMsg != "" {
			lastErr = errMsg
		}
	}

	msg := "Unable to fetch Kiro usage right now."
	if lastErr != "" {
		msg = fmt.Sprintf("Unable to fetch Kiro usage right now. (%s)", lastErr)
	}

	return &QuotaResult{
		Provider:           "kiro",
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
		Quotas:             map[string]QuotaItem{},
	}, nil
}

func setKiroHeaders(h http.Header, token string, isAPIKey, isExternalIDP bool) {
	h.Set("Authorization", "Bearer "+token)
	h.Set("Accept", "application/json")
	h.Set("x-amz-user-agent", "aws-sdk-js/1.0.0 KiroIDE")
	h.Set("user-agent", "aws-sdk-js/1.0.0 KiroIDE")

	if isAPIKey {
		h.Set("tokentype", "API_KEY")
	}

	if isExternalIDP {
		h.Set("TokenType", "EXTERNAL_IDP")
	}
}

func parseKiroBreakdownItem(b map[string]any, resetAt *string, quotas map[string]QuotaItem) {
	resType := strings.ToLower(fmt.Sprintf("%v", b["resourceType"]))
	if resType == "" || resType == "<nil>" {
		resType = "unknown"
	}

	used := toFiniteFloat(b["currentUsageWithPrecision"], 0)
	total := toFiniteFloat(b["usageLimitWithPrecision"], 0)

	quotas[resType] = makeQuota(used, total, resetAt, false)

	var fti map[string]any
	if val, ok := b["freeTrialInfo"].(map[string]any); ok {
		fti = val
	}

	if fti != nil {
		freeUsed := toFiniteFloat(fti["currentUsageWithPrecision"], 0)
		freeTotal := toFiniteFloat(fti["usageLimitWithPrecision"], 0)

		freeReset := parseResetTime(fti["freeTrialExpiry"])
		if freeReset == nil {
			freeReset = resetAt
		}

		quotas[resType+"_freetrial"] = makeQuota(freeUsed, freeTotal, freeReset, false)
	}
}

func parseKiroQuotaData(data map[string]any) *QuotaResult {
	var breakdowns []any
	if val, ok := data["usageBreakdownList"].([]any); ok {
		breakdowns = val
	}

	quotas := make(map[string]QuotaItem)

	resetAt := parseResetTime(data["nextDateReset"])
	if resetAt == nil {
		resetAt = parseResetTime(data["resetDate"])
	}

	for _, item := range breakdowns {
		b, ok := item.(map[string]any)
		if !ok {
			continue
		}

		parseKiroBreakdownItem(b, resetAt, quotas)
	}

	plan := "Kiro"

	var subInfo map[string]any
	if val, ok := data["subscriptionInfo"].(map[string]any); ok {
		subInfo = val
	}

	if subInfo != nil {
		if t, ok := subInfo["subscriptionTitle"].(string); ok && t != "" {
			plan = t
		}
	}

	return &QuotaResult{
		Provider:           "kiro",
		Plan:               plan,
		Limit:              0,
		Used:               0,
		Remaining:          0,
		TotalUsagePct:      0,
		LimitReached:       nil,
		ReviewLimitReached: nil,
		IsQuotaExceeded:    nil,
		ResetCredits:       nil,
		ResetsAt:           resetAt,
		Message:            "",
		Details:            nil,
		Quotas:             quotas,
	}
}
