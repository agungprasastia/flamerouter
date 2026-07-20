package store

import "database/sql"

func (s *Store) ListAliases() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT alias, target_model FROM model_aliases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var alias, target string
		if err := rows.Scan(&alias, &target); err != nil {
			return nil, err
		}
		out[alias] = target
	}
	return out, rows.Err()
}

func (s *Store) SetAlias(alias, targetModel string) error {
	_, err := s.db.Exec(
		`INSERT INTO model_aliases(alias, target_model) VALUES(?,?)
		 ON CONFLICT(alias) DO UPDATE SET target_model=excluded.target_model`,
		alias, targetModel,
	)
	return err
}

func (s *Store) GetProviderNodes() (nodes []ProviderNode, err error) {
	rows, err := s.db.Query(`SELECT id, type, name, prefix, api_type, base_url FROM provider_nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n ProviderNode
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.Prefix, &n.APIType, &n.BaseURL); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

type ProviderNode struct {
	ID      string
	Type    string
	Name    string
	Prefix  string
	APIType string
	BaseURL string
}

func (s *Store) CreateProviderNode(typ, name, prefix, apiType, baseURL string) (string, error) {
	id := newID()
	_, err := s.db.Exec(
		`INSERT INTO provider_nodes(id, type, name, prefix, api_type, base_url) VALUES(?,?,?,?,?,?)`,
		id, typ, name, prefix, apiType, baseURL,
	)
	return id, err
}

func (s *Store) UpdateProviderNode(id, name, prefix, apiType, baseURL string) error {
	res, err := s.db.Exec(
		`UPDATE provider_nodes SET name=?, prefix=?, api_type=?, base_url=? WHERE id=?`,
		name, prefix, apiType, baseURL, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeleteProviderNode(id string) error {
	res, err := s.db.Exec(`DELETE FROM provider_nodes WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
