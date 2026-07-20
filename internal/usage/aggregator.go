package usage

import (
	"time"

	"flamerouter/internal/store"
)

// DefaultRange returns [from,to] for the last 30 days (UTC).
func DefaultRange() (from, to string) {
	now := time.Now().UTC()
	to = now.Format("2006-01-02")
	from = now.AddDate(0, 0, -30).Format("2006-01-02")
	return
}

// SumDaily aggregates totals across daily rows.
func SumDaily(rows []store.UsageDaily) (requests, prompt, completion int) {
	for _, r := range rows {
		requests += r.Requests
		prompt += r.PromptTokens
		completion += r.CompletionTokens
	}
	return
}
