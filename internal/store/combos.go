package store

import (
	"database/sql"
	"encoding/json"
)

type Combo struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Models []string `json:"models"`
}

func (s *Store) ListCombos() ([]Combo, error) {
	rows, err := s.db.Query(`SELECT id, name, models FROM combos ORDER BY name`)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var out []Combo

	for rows.Next() {
		var c Combo

		var modelsJSON string
		if err := rows.Scan(&c.ID, &c.Name, &modelsJSON); err != nil {
			return nil, err
		}

		_ = json.Unmarshal([]byte(modelsJSON), &c.Models)
		out = append(out, c)
	}

	return out, rows.Err()
}

func (s *Store) GetComboByName(name string) (*Combo, error) {
	var c Combo

	var modelsJSON string

	err := s.db.QueryRow(`SELECT id, name, models FROM combos WHERE name=?`, name).
		Scan(&c.ID, &c.Name, &modelsJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	_ = json.Unmarshal([]byte(modelsJSON), &c.Models)

	return &c, nil
}

func (s *Store) CreateCombo(name string, models []string) (string, error) {
	id := newID()
	b, _ := json.Marshal(models)
	_, err := s.db.Exec(
		`INSERT INTO combos(id, name, models) VALUES(?,?,?)`,
		id, name, string(b),
	)

	return id, err
}

func (s *Store) UpdateCombo(id, name string, models []string) error {
	b, _ := json.Marshal(models)

	res, err := s.db.Exec(`UPDATE combos SET name=?, models=? WHERE id=?`, name, string(b), id)
	if err != nil {
		return err
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (s *Store) DeleteCombo(id string) error {
	res, err := s.db.Exec(`DELETE FROM combos WHERE id=?`, id)
	if err != nil {
		return err
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}

	return nil
}
