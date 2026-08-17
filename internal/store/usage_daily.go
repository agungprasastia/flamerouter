// Package store provides SQLite persistent storage for flamerouter state.
package store

// UsageDaily aggregates token and request metrics per day.
type UsageDaily struct {
	Date, Provider, Model          string
	Requests                       int
	PromptTokens, CompletionTokens int
}

// InsertUsageDaily upserts daily usage metrics.
func (s *Store) InsertUsageDaily(date, provider, model string, requests, prompt, completion int) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_daily(date, provider, model, requests, prompt_tokens, completion_tokens)
		 VALUES(?,?,?,?,?,?)
		 ON CONFLICT(date, provider, model) DO UPDATE SET
		   requests = requests + excluded.requests,
		   prompt_tokens = prompt_tokens + excluded.prompt_tokens,
		   completion_tokens = completion_tokens + excluded.completion_tokens`,
		date, provider, model, requests, prompt, completion,
	)

	return err
}

// QueryUsageDaily queries daily usage within a date range.
func (s *Store) QueryUsageDaily(from, to string) ([]UsageDaily, error) {
	return s.queryUsageWithSQL(
		`SELECT date, provider, model, requests, prompt_tokens, completion_tokens
		 FROM usage_daily WHERE date>=? AND date<=? ORDER BY date, provider, model`,
		from, to,
	)
}

// QueryUsageChart aggregates requests/tokens by date for charts.
func (s *Store) QueryUsageChart(from, to string) ([]UsageDaily, error) {
	return s.queryUsageWithSQL(
		`SELECT date, '', '', SUM(requests), SUM(prompt_tokens), SUM(completion_tokens)
		 FROM usage_daily WHERE date>=? AND date<=?
		 GROUP BY date ORDER BY date`,
		from, to,
	)
}

func (s *Store) queryUsageWithSQL(queryStr, from, to string) ([]UsageDaily, error) {
	rows, err := s.db.Query(queryStr, from, to)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []UsageDaily

	for rows.Next() {
		var u UsageDaily
		if err := rows.Scan(&u.Date, &u.Provider, &u.Model, &u.Requests, &u.PromptTokens, &u.CompletionTokens); err != nil {
			return nil, err
		}

		out = append(out, u)
	}

	return out, rows.Err()
}
