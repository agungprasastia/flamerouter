// Package store provides SQLite persistent storage for flamerouter state.
package store

import "database/sql"

// KVGet retrieves a key value from a scope.
func (s *Store) KVGet(scope, key string) (string, error) {
	var v string

	err := s.db.QueryRow(`SELECT value FROM kv WHERE scope=? AND key=?`, scope, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}

	return v, err
}

// KVSet sets a key value in a scope.
func (s *Store) KVSet(scope, key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO kv(scope, key, value) VALUES(?,?,?)
		 ON CONFLICT(scope, key) DO UPDATE SET value=excluded.value`,
		scope, key, value,
	)

	return err
}

// KVDelete removes a key from a scope.
func (s *Store) KVDelete(scope, key string) error {
	_, err := s.db.Exec(`DELETE FROM kv WHERE scope=? AND key=?`, scope, key)
	return err
}

// KVList retrieves all key-value pairs in a scope.
func (s *Store) KVList(scope string) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM kv WHERE scope=?`, scope)
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
