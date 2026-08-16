package store

type UsageDaily struct {
	Date, Provider, Model          string
	Requests                       int
	PromptTokens, CompletionTokens int
}

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

func (s *Store) QueryUsageDaily(from, to string) ([]UsageDaily, error) {
	rows, err := s.db.Query(
		`SELECT date, provider, model, requests, prompt_tokens, completion_tokens
		 FROM usage_daily WHERE date>=? AND date<=? ORDER BY date, provider, model`,
		from, to,
	)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

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

// QueryUsageChart aggregates requests/tokens by date for charts.
func (s *Store) QueryUsageChart(from, to string) ([]UsageDaily, error) {
	rows, err := s.db.Query(
		`SELECT date, '', '', SUM(requests), SUM(prompt_tokens), SUM(completion_tokens)
		 FROM usage_daily WHERE date>=? AND date<=?
		 GROUP BY date ORDER BY date`,
		from, to,
	)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

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
