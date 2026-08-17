// Package store provides SQLite persistent storage for flamerouter state.
package store

// ListDisabledModels returns all disabled model names.
func (s *Store) ListDisabledModels() ([]string, error) {
	rows, err := s.db.Query(`SELECT model FROM disabled_models ORDER BY model`)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []string

	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// DisableModel adds a model to disabled models.
func (s *Store) DisableModel(model string) error {
	_, err := s.db.Exec(
		`INSERT INTO disabled_models(model) VALUES(?) ON CONFLICT(model) DO NOTHING`,
		model,
	)

	return err
}

// EnableModel removes a model from disabled models.
func (s *Store) EnableModel(model string) error {
	_, err := s.db.Exec(`DELETE FROM disabled_models WHERE model=?`, model)
	return err
}
