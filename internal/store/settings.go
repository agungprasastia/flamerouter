// Package store provides SQLite persistent storage for flamerouter state.
package store

import "database/sql"

// GetSetting retrieves a configuration setting by key.
func (s *Store) GetSetting(key string) (string, error) {
	var v string

	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}

	if err != nil {
		return "", err
	}

	return v, nil
}

// SetSetting sets a configuration setting value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)

	return err
}

// ListSettings returns all configuration settings as a map.
func (s *Store) ListSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	out := make(map[string]string)

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}

		out[k] = v
	}

	return out, rows.Err()
}
