// Package store provides SQLite persistent storage for flamerouter state.
package store

import "database/sql"

// CustomModel describes custom model definitions.
type CustomModel struct {
	ID, Provider, ModelID, DisplayName, Capabilities string
}

// ListCustomModels returns all custom models.
func (s *Store) ListCustomModels() ([]CustomModel, error) {
	rows, err := s.db.Query(
		`SELECT id, provider, model_id, COALESCE(display_name,''), COALESCE(capabilities,'{}') FROM custom_models ORDER BY provider, model_id`,
	)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []CustomModel

	for rows.Next() {
		var m CustomModel
		if err := rows.Scan(&m.ID, &m.Provider, &m.ModelID, &m.DisplayName, &m.Capabilities); err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

// CreateCustomModel inserts a custom model.
func (s *Store) CreateCustomModel(provider, modelID, displayName, capabilities string) (string, error) {
	id := newID()

	if capabilities == "" {
		capabilities = "{}"
	}

	_, err := s.db.Exec(
		`INSERT INTO custom_models(id, provider, model_id, display_name, capabilities) VALUES(?,?,?,?,?)`,
		id, provider, modelID, displayName, capabilities,
	)

	return id, err
}

// DeleteCustomModel deletes a custom model by id.
func (s *Store) DeleteCustomModel(id string) error {
	res, err := s.db.Exec(`DELETE FROM custom_models WHERE id=?`, id)
	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if n == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// DeleteCustomModelByModel deletes a custom model by provider and model ID.
func (s *Store) DeleteCustomModelByModel(provider, modelID string) error {
	res, err := s.db.Exec(`DELETE FROM custom_models WHERE provider=? AND model_id=?`, provider, modelID)
	if err != nil {
		return err
	}

	n, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if n == 0 {
		return sql.ErrNoRows
	}

	return nil
}
