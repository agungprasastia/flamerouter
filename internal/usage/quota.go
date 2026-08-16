package usage

import (
	"context"
	"net/http"
	"time"
)

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

type ResetCreditInfo struct {
	AvailableCount int `json:"availableCount"`
}

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

type FetchOptions struct {
	ProviderSpecificData map[string]any
	HTTPClient           *http.Client
	Provider             string
	AccessToken          string
	APIKey               string
	BaseURL              string
	Force                bool
}

type QuotaHandler func(ctx context.Context, opts FetchOptions) (*QuotaResult, error)

var (
	quotaHandlers   = map[string]QuotaHandler{}
	quotaHTTPClient = &http.Client{
		Timeout: 15 * time.Second,
	}
)

func RegisterQuotaHandler(provider string, handler QuotaHandler) {
	quotaHandlers[provider] = handler
}

func FetchProviderUsage(ctx context.Context, opts FetchOptions) *QuotaResult {
	if opts.HTTPClient == nil {
		opts.HTTPClient = quotaHTTPClient
	}

	handler, ok := quotaHandlers[opts.Provider]
	if !ok {
		return &QuotaResult{
			Provider: opts.Provider,
			Message:  "Usage API not implemented for " + opts.Provider,
		}
	}

	res, err := handler(ctx, opts)
	if err != nil {
		return &QuotaResult{
			Provider: opts.Provider,
			Message:  err.Error(),
		}
	}

	if res == nil {
		return &QuotaResult{
			Provider: opts.Provider,
		}
	}

	if res.Provider == "" {
		res.Provider = opts.Provider
	}

	computeTopLevelNormalized(res)

	return res
}

func FetchQuota(provider string) QuotaResult {
	res := FetchProviderUsage(context.Background(), FetchOptions{
		Provider: provider,
	})
	if res == nil {
		return QuotaResult{Provider: provider}
	}

	return *res
}

func computeTopLevelNormalized(res *QuotaResult) {
	if len(res.Quotas) == 0 {
		return
	}

	if res.Limit == 0 && res.Used == 0 && res.Remaining == 0 {
		var chosen *QuotaItem

		for _, k := range []string{"session", "Session (5h)", "On-demand", "Monthly included", "chat", "user", "Weekly", "Weekly (7d)"} {
			if item, exists := res.Quotas[k]; exists {
				chosen = &item
				break
			}
		}

		if chosen == nil {
			for _, item := range res.Quotas {
				chosen = &item
				break
			}
		}

		if chosen != nil {
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
	}
}
