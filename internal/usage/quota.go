// Package usage tracks provider quotas, request metrics, and streaming telemetry.
package usage

import (
	"context"
	"net/http"
	"time"
)

// QuotaItem represents a specific quota bucket with used, total, and remaining amounts.
type QuotaItem struct {
	ResetAt             *string `json:"resetAt,omitempty"`
	Recurring           *bool   `json:"recurring,omitempty"`
	DisplayName         string  `json:"displayName,omitempty"`
	Unit                string  `json:"unit,omitempty"`
	Used                float64 `json:"used"`
	Total               float64 `json:"total"`
	Remaining           float64 `json:"remaining,omitempty"`
	RemainingPercentage float64 `json:"remainingPercentage"`
	Unlimited           bool    `json:"unlimited,omitempty"`
}

// ResetCreditInfo contains count of available reset credits.
type ResetCreditInfo struct {
	AvailableCount int `json:"availableCount"`
}

// QuotaResult represents the unified quota details for a provider.
type QuotaResult struct {
	Quotas             map[string]QuotaItem `json:"quotas,omitempty"`
	Details            map[string]any       `json:"details,omitempty"`
	IsQuotaExceeded    *bool                `json:"isQuotaExceeded,omitempty"`
	ReviewLimitReached *bool                `json:"reviewLimitReached,omitempty"`
	ResetsAt           *string              `json:"resetsAt,omitempty"`
	LimitReached       *bool                `json:"limitReached,omitempty"`
	ResetCredits       *ResetCreditInfo     `json:"resetCredits,omitempty"`
	Message            string               `json:"message,omitempty"`
	Provider           string               `json:"provider"`
	Plan               string               `json:"plan,omitempty"`
	Remaining          int64                `json:"remaining"`
	TotalUsagePct      float64              `json:"totalUsagePercentage,omitempty"`
	Used               int64                `json:"used"`
	Limit              int64                `json:"limit"`
}

// FetchOptions contains credentials and options for fetching quota from a provider.
type FetchOptions struct {
	ProviderSpecificData map[string]any
	HTTPClient           *http.Client
	Provider             string
	AccessToken          string
	APIKey               string
	BaseURL              string
	Force                bool
}

// QuotaHandler is a function that fetches quota for a provider.
type QuotaHandler func(ctx context.Context, opts FetchOptions) (*QuotaResult, error)

var (
	quotaHandlers   = map[string]QuotaHandler{}
	quotaHTTPClient = &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       15 * time.Second,
	}
)

// RegisterQuotaHandler registers a quota retrieval handler for a provider.
func RegisterQuotaHandler(provider string, handler QuotaHandler) {
	quotaHandlers[provider] = handler
}

func makeEmptyQuotaResult(provider, message string) *QuotaResult {
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
		Message:            message,
		Details:            nil,
		Quotas:             nil,
	}
}

// FetchProviderUsage queries the registered handler to obtain current quota usage.
func FetchProviderUsage(ctx context.Context, opts FetchOptions) *QuotaResult {
	if opts.HTTPClient == nil {
		opts.HTTPClient = quotaHTTPClient
	}

	handler, ok := quotaHandlers[opts.Provider]
	if !ok {
		return makeEmptyQuotaResult(opts.Provider, "Usage API not implemented for "+opts.Provider)
	}

	res, err := handler(ctx, opts)
	if err != nil {
		return makeEmptyQuotaResult(opts.Provider, err.Error())
	}

	if res == nil {
		return makeEmptyQuotaResult(opts.Provider, "")
	}

	if res.Provider == "" {
		res.Provider = opts.Provider
	}

	computeTopLevelNormalized(res)

	return res
}

// FetchQuota fetches quota for a provider with default options.
func FetchQuota(provider string) QuotaResult {
	res := FetchProviderUsage(context.Background(), FetchOptions{
		Provider:             provider,
		AccessToken:          "",
		APIKey:               "",
		BaseURL:              "",
		ProviderSpecificData: nil,
		HTTPClient:           nil,
		Force:                false,
	})
	if res == nil {
		return QuotaResult{
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
			Message:            "",
			Details:            nil,
			Quotas:             nil,
		}
	}

	return *res
}

func selectCandidateQuota(quotas map[string]QuotaItem) *QuotaItem {
	for _, k := range []string{"session", "Session (5h)", "On-demand", "Monthly included", "chat", "user", "Weekly", "Weekly (7d)"} {
		if item, exists := quotas[k]; exists {
			return &item
		}
	}

	for _, item := range quotas {
		return &item
	}

	return nil
}

func computeTopLevelNormalized(res *QuotaResult) {
	if len(res.Quotas) == 0 || (res.Limit != 0 || res.Used != 0 || res.Remaining != 0) {
		return
	}

	chosen := selectCandidateQuota(res.Quotas)
	if chosen == nil {
		return
	}

	res.Limit = int64(chosen.Total)
	res.Used = int64(chosen.Used)

	if chosen.Remaining > 0 {
		res.Remaining = int64(chosen.Remaining)
	} else if chosen.Total >= chosen.Used {
		res.Remaining = int64(chosen.Total - chosen.Used)
	}

	if chosen.ResetAt != nil && res.ResetsAt == nil {
		res.ResetsAt = chosen.ResetAt
	}
}
