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

func fetchKiroUsage(ctx context.Context, opts FetchOptions) (*QuotaResult, error) {
	if opts.AccessToken == "" {
		return &QuotaResult{Plan: "Kiro", Message: "Kiro access token not available."}, nil
	}

	authMethod, _ := opts.ProviderSpecificData["authMethod"].(string)
	if authMethod == "" {
		authMethod = "builder-id"
	}

	isAPIKey := authMethod == "api_key"
	isExternalIDP := authMethod == "external_idp"

	profileArn, _ := opts.ProviderSpecificData["profileArn"].(string)

	params := url.Values{
		"isEmailRequired": {"true"},
		"origin":          {"AI_EDITOR"},
		"resourceType":    {"AGENTIC_REQUEST"},
	}

	cwGetURL := fmt.Sprintf("%s%s?%s", kiroCwHost, kiroLimitsPath, params.Encode())

	type attempt struct {
		exec func() (*http.Response, error)
		name string
	}

	attempts := []attempt{
		{
			name: "codewhisperer-get",
			exec: func() (*http.Response, error) {
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, cwGetURL, nil)
				if err != nil {
					return nil, err
				}
				setKiroHeaders(req.Header, opts.AccessToken, isAPIKey, isExternalIDP)
				return doHTTP(opts.HTTPClient, req)
			},
		},
		{
			name: "codewhisperer-post",
			exec: func() (*http.Response, error) {
				bodyMap := map[string]any{
					"origin":       "AI_EDITOR",
					"resourceType": "AGENTIC_REQUEST",
				}
				if profileArn != "" {
					bodyMap["profileArn"] = profileArn
				}
				bodyBytes, _ := json.Marshal(bodyMap)
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, kiroCwHost, bytes.NewReader(bodyBytes))
				if err != nil {
					return nil, err
				}
				setKiroHeaders(req.Header, opts.AccessToken, isAPIKey, isExternalIDP)
				req.Header.Set("Content-Type", "application/x-amz-json-1.0")
				req.Header.Set("x-amz-target", "AmazonCodeWhispererService.GetUsageLimits")
				return doHTTP(opts.HTTPClient, req)
			},
		},
		{
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
		},
	}

	var lastErr string

	for _, att := range attempts {
		res, err := att.exec()
		if err != nil {
			lastErr = err.Error()
			continue
		}
		defer res.Body.Close()

		if res.StatusCode == 401 || res.StatusCode == 403 {
			if authMethod == "idc" {
				return &QuotaResult{
					Message: "Kiro quota API is unavailable for the current AWS IAM Identity Center session. Chat may still work. If this persists after renewing your session, reconnect Kiro.",
					Quotas:  map[string]QuotaItem{},
				}, nil
			}

			return &QuotaResult{
				Message: "Kiro quota API authentication expired. Chat may still work.",
				Quotas:  map[string]QuotaItem{},
			}, nil
		}

		if res.StatusCode < 200 || res.StatusCode >= 300 {
			errBytes, _ := io.ReadAll(io.LimitReader(res.Body, 512))
			lastErr = fmt.Sprintf("%s: %d %s", att.name, res.StatusCode, strings.TrimSpace(string(errBytes)))

			continue
		}

		var data map[string]any
		if err := json.NewDecoder(res.Body).Decode(&data); err == nil {
			return parseKiroQuotaData(data), nil
		}
	}

	if lastErr != "" {
		return &QuotaResult{
			Message: fmt.Sprintf("Unable to fetch Kiro usage right now. (%s)", lastErr),
			Quotas:  map[string]QuotaItem{},
		}, nil
	}

	return &QuotaResult{
		Message: "Unable to fetch Kiro usage right now.",
		Quotas:  map[string]QuotaItem{},
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

func parseKiroQuotaData(data map[string]any) *QuotaResult {
	breakdowns, _ := data["usageBreakdownList"].([]any)
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

		resType := strings.ToLower(fmt.Sprintf("%v", b["resourceType"]))
		if resType == "" || resType == "<nil>" {
			resType = "unknown"
		}

		used := toFiniteFloat(b["currentUsageWithPrecision"], 0)
		total := toFiniteFloat(b["usageLimitWithPrecision"], 0)

		quotas[resType] = makeQuota(used, total, resetAt, false)

		if fti, ok := b["freeTrialInfo"].(map[string]any); ok && fti != nil {
			freeUsed := toFiniteFloat(fti["currentUsageWithPrecision"], 0)
			freeTotal := toFiniteFloat(fti["usageLimitWithPrecision"], 0)

			freeReset := parseResetTime(fti["freeTrialExpiry"])
			if freeReset == nil {
				freeReset = resetAt
			}

			quotas[resType+"_freetrial"] = makeQuota(freeUsed, freeTotal, freeReset, false)
		}
	}

	plan := "Kiro"

	if subInfo, ok := data["subscriptionInfo"].(map[string]any); ok {
		if t, ok := subInfo["subscriptionTitle"].(string); ok && t != "" {
			plan = t
		}
	}

	return &QuotaResult{
		Provider: "kiro",
		Plan:     plan,
		Quotas:   quotas,
		ResetsAt: resetAt,
	}
}
