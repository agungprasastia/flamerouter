package store

type RequestDetail struct {
	ID, Timestamp, Provider, Model, ConnectionID string
	StatusCode, DurationMs                       int
	PromptTokens, CompletionTokens               int
	RequestBody, ResponsePreview, ErrorText      string
	Client, SourceFormat, TargetFormat           string
}

func (s *Store) InsertRequestDetail(d RequestDetail) error {
	if d.ID == "" {
		d.ID = newID()
	}
	_, err := s.db.Exec(
		`INSERT INTO request_details(
			id, timestamp, provider, model, connection_id, status_code, duration_ms,
			prompt_tokens, completion_tokens, request_body, response_preview, error_text,
			client, source_format, target_format
		) VALUES(?, COALESCE(NULLIF(?,''), datetime('now')),?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Timestamp, d.Provider, d.Model, d.ConnectionID,
		d.StatusCode, d.DurationMs, d.PromptTokens, d.CompletionTokens,
		d.RequestBody, d.ResponsePreview, d.ErrorText,
		d.Client, d.SourceFormat, d.TargetFormat,
	)
	return err
}

func (s *Store) QueryRequestDetails(limit int) ([]RequestDetail, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT id, COALESCE(timestamp,''), provider, model, COALESCE(connection_id,''),
		        COALESCE(status_code,0), COALESCE(duration_ms,0),
		        COALESCE(prompt_tokens,0), COALESCE(completion_tokens,0),
		        COALESCE(request_body,''), COALESCE(response_preview,''), COALESCE(error_text,''),
		        COALESCE(client,''), COALESCE(source_format,''), COALESCE(target_format,'')
		 FROM request_details ORDER BY timestamp DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RequestDetail
	for rows.Next() {
		var d RequestDetail
		if err := rows.Scan(
			&d.ID, &d.Timestamp, &d.Provider, &d.Model, &d.ConnectionID,
			&d.StatusCode, &d.DurationMs, &d.PromptTokens, &d.CompletionTokens,
			&d.RequestBody, &d.ResponsePreview, &d.ErrorText,
			&d.Client, &d.SourceFormat, &d.TargetFormat,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
