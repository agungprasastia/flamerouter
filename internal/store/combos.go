// Package store provides SQLite persistent storage for flamerouter state.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Combo represents a model fallbacks / combo configuration.
type Combo struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

// ListCombos returns all combos ordered by name.
func (s *Store) ListCombos() ([]Combo, error) {
	rows, err := s.db.Query(`SELECT id, name, models FROM combos ORDER BY name`)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []Combo

	for rows.Next() {
		var c Combo

		var modelsJSON string
		if err := rows.Scan(&c.ID, &c.Name, &modelsJSON); err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(modelsJSON), &c.Models); err != nil {
			c.Models = nil
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// GetComboByName retrieves a combo by name or returns nil, nil if not found.
//
//nolint:nilnil // returning nil combo on ErrNoRows is by design
func (s *Store) GetComboByName(name string) (*Combo, error) {
	var c Combo

	var modelsJSON string

	err := s.db.QueryRow(`SELECT id, name, models FROM combos WHERE name=?`, name).
		Scan(&c.ID, &c.Name, &modelsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	if err := json.Unmarshal([]byte(modelsJSON), &c.Models); err != nil {
		c.Models = nil
	}

	return &c, nil
}

// CreateCombo saves a new combo model list.
func (s *Store) CreateCombo(name string, models []string) (string, error) {
	id := newID()

	b, err := json.Marshal(models)
	if err != nil {
		return "", err
	}

	_, err = s.db.Exec(
		`INSERT INTO combos(id, name, models) VALUES(?,?,?)`,
		id, name, string(b),
	)

	return id, err
}

// UpdateCombo updates models list for a combo id.
func (s *Store) UpdateCombo(id, name string, models []string) error {
	b, err := json.Marshal(models)
	if err != nil {
		return err
	}

	res, err := s.db.Exec(`UPDATE combos SET name=?, models=? WHERE id=?`, name, string(b), id)
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

// DeleteCombo deletes a combo by id.
func (s *Store) DeleteCombo(id string) error {
	res, err := s.db.Exec(`DELETE FROM combos WHERE id=?`, id)
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
