// Package store provides SQLite persistent storage for flamerouter state.
package store

import "database/sql"

// ListAliases returns all model aliases mapped to target models.
func (s *Store) ListAliases() (map[string]string, error) {
	aliasMap := make(map[string]string)

	rows, err := s.db.Query(`SELECT alias, target_model FROM model_aliases`)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	for rows.Next() {
		var (
			aliasName  string
			targetName string
		)

		if scanErr := rows.Scan(&aliasName, &targetName); scanErr != nil {
			return nil, scanErr
		}

		aliasMap[aliasName] = targetName
	}

	return aliasMap, rows.Err()
}

// DeleteAlias removes a model alias.
func (s *Store) DeleteAlias(alias string) error {
	_, err := s.db.Exec(`DELETE FROM model_aliases WHERE alias=?`, alias)
	return err
}

// SetAlias creates or updates a model alias.
func (s *Store) SetAlias(alias, targetModel string) error {
	_, err := s.db.Exec(
		`INSERT INTO model_aliases(alias, target_model) VALUES(?,?)
		 ON CONFLICT(alias) DO UPDATE SET target_model=excluded.target_model`,
		alias, targetModel,
	)

	return err
}

// GetProviderNodes returns all provider nodes.
func (s *Store) GetProviderNodes() (nodes []ProviderNode, err error) {
	rows, err := s.db.Query(`SELECT id, type, name, prefix, api_type, base_url FROM provider_nodes`)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	for rows.Next() {
		var n ProviderNode
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.Prefix, &n.APIType, &n.BaseURL); err != nil {
			return nil, err
		}

		nodes = append(nodes, n)
	}

	return nodes, rows.Err()
}

// ProviderNode describes a remote or local upstream provider node.
type ProviderNode struct {
	ID      string
	Type    string
	Name    string
	Prefix  string
	APIType string
	BaseURL string
}

// CreateProviderNode inserts a new provider node.
func (s *Store) CreateProviderNode(typ, name, prefix, apiType, baseURL string) (string, error) {
	id := newID()
	_, err := s.db.Exec(
		`INSERT INTO provider_nodes(id, type, name, prefix, api_type, base_url) VALUES(?,?,?,?,?,?)`,
		id, typ, name, prefix, apiType, baseURL,
	)

	return id, err
}

// UpdateProviderNode updates an existing provider node.
func (s *Store) UpdateProviderNode(id, name, prefix, apiType, baseURL string) error {
	res, err := s.db.Exec(
		`UPDATE provider_nodes SET name=?, prefix=?, api_type=?, base_url=? WHERE id=?`,
		name, prefix, apiType, baseURL, id,
	)
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

// DeleteProviderNode removes a provider node.
func (s *Store) DeleteProviderNode(id string) error {
	res, err := s.db.Exec(`DELETE FROM provider_nodes WHERE id=?`, id)
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
