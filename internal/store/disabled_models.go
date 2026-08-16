package store

func (s *Store) ListDisabledModels() ([]string, error) {
	rows, err := s.db.Query(`SELECT model FROM disabled_models ORDER BY model`)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

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

func (s *Store) DisableModel(model string) error {
	_, err := s.db.Exec(
		`INSERT INTO disabled_models(model) VALUES(?) ON CONFLICT(model) DO NOTHING`,
		model,
	)

	return err
}

func (s *Store) EnableModel(model string) error {
	_, err := s.db.Exec(`DELETE FROM disabled_models WHERE model=?`, model)
	return err
}
