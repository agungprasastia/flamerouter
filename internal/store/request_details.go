// Package store provides SQLite persistent storage for flamerouter state.
package store

// RequestDetail holds detailed telemetry about a proxied request.
type RequestDetail struct {
	RequestBody      string
	ResponsePreview  string
	Provider         string
	Model            string
	ConnectionID     string
	TargetFormat     string
	Timestamp        string
	SourceFormat     string
	Client           string
	ID               string
	ErrorText        string
	Cost             float64
	PromptTokens     int
	DurationMs       int
	CompletionTokens int
	CachedTokens     int
	StatusCode       int
}

// InsertRequestDetail records request telemetry to SQLite.
func (s *Store) InsertRequestDetail(d RequestDetail) error {
	if d.ID == "" {
		d.ID = newID()
	}

	_, err := s.db.Exec(
		`INSERT INTO request_details(
			id, timestamp, provider, model, connection_id, status_code, duration_ms,
			prompt_tokens, completion_tokens, cached_tokens, cost, request_body, response_preview, error_text,
			client, source_format, target_format
		) VALUES(?, COALESCE(NULLIF(?,''), datetime('now')),?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Timestamp, d.Provider, d.Model, d.ConnectionID,
		d.StatusCode, d.DurationMs, d.PromptTokens, d.CompletionTokens, d.CachedTokens, d.Cost,
		d.RequestBody, d.ResponsePreview, d.ErrorText,
		d.Client, d.SourceFormat, d.TargetFormat,
	)

	return err
}

// QueryRequestDetails returns the most recent request details.
func (s *Store) QueryRequestDetails(limit int) ([]RequestDetail, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(
		`SELECT id, COALESCE(timestamp,''), provider, model, COALESCE(connection_id,''),
		        COALESCE(status_code,0), COALESCE(duration_ms,0),
		        COALESCE(prompt_tokens,0), COALESCE(completion_tokens,0), COALESCE(cached_tokens,0), COALESCE(cost,0.0),
		        COALESCE(request_body,''), COALESCE(response_preview,''), COALESCE(error_text,''),
		        COALESCE(client,''), COALESCE(source_format,''), COALESCE(target_format,'')
		 FROM request_details ORDER BY timestamp DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []RequestDetail

	for rows.Next() {
		var d RequestDetail
		if err := rows.Scan(
			&d.ID, &d.Timestamp, &d.Provider, &d.Model, &d.ConnectionID,
			&d.StatusCode, &d.DurationMs, &d.PromptTokens, &d.CompletionTokens, &d.CachedTokens, &d.Cost,
			&d.RequestBody, &d.ResponsePreview, &d.ErrorText,
			&d.Client, &d.SourceFormat, &d.TargetFormat,
		); err != nil {
			return nil, err
		}

		out = append(out, d)
	}

	return out, rows.Err()
}

// QueryRequestDetailsByConnection returns the most recent request details for a given connection ID.
func (s *Store) QueryRequestDetailsByConnection(connID string, limit int) ([]RequestDetail, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(
		`SELECT id, COALESCE(timestamp,''), provider, model, COALESCE(connection_id,''),
		        COALESCE(status_code,0), COALESCE(duration_ms,0),
		        COALESCE(prompt_tokens,0), COALESCE(completion_tokens,0)
		 FROM request_details WHERE connection_id = ? ORDER BY timestamp DESC LIMIT ?`,
		connID, limit,
	)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []RequestDetail

	for rows.Next() {
		var d RequestDetail
		if err := rows.Scan(
			&d.ID, &d.Timestamp, &d.Provider, &d.Model, &d.ConnectionID,
			&d.StatusCode, &d.DurationMs, &d.PromptTokens, &d.CompletionTokens,
		); err != nil {
			return nil, err
		}

		out = append(out, d)
	}

	return out, rows.Err()
}
