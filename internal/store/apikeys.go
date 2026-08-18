// Package store provides SQLite persistent storage for flamerouter state.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"
)

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}

	return hex.EncodeToString(b)
}

// CreateAPIKey persists a new API key hash and returns the generated UUID.
func (s *Store) CreateAPIKey(name, keyID, keyHash, machineID string) (string, error) {
	id := newID()
	_, err := s.db.Exec(
		`INSERT INTO api_keys(id, name, key_id, key_hash, machine_id, is_active, created_at)
		 VALUES(?,?,?,?,?,1,?)`,
		id, name, keyID, keyHash, machineID, time.Now().UTC().Format(time.RFC3339),
	)

	return id, err
}

// LookupActiveByKeyID finds an active key by keyID.
func (s *Store) LookupActiveByKeyID(keyID string) (keyHash, machineID string, ok bool, err error) {
	err = s.db.QueryRow(
		`SELECT key_hash, COALESCE(machine_id,'') FROM api_keys WHERE key_id=? AND is_active=1`,
		keyID,
	).Scan(&keyHash, &machineID)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}

	if err != nil {
		return "", "", false, err
	}

	return keyHash, machineID, true, nil
}

// APIKey represents stored API key metadata.
type APIKey struct {
	ID        string
	Name      string
	KeyID     string
	MachineID string
	CreatedAt string
	IsActive  bool
}

// ListAPIKeys returns all API keys ordered by creation time descending.
func (s *Store) ListAPIKeys() ([]APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id, name, key_id, COALESCE(machine_id,''), is_active, created_at FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []APIKey

	for rows.Next() {
		var k APIKey

		var active int
		if err := rows.Scan(&k.ID, &k.Name, &k.KeyID, &k.MachineID, &active, &k.CreatedAt); err != nil {
			return nil, err
		}

		k.IsActive = active != 0
		out = append(out, k)
	}

	return out, rows.Err()
}

// UpdateAPIKey updates an API key's active status.
func (s *Store) UpdateAPIKey(id string, isActive bool) error {
	res, err := s.db.Exec(`UPDATE api_keys SET is_active=? WHERE id=?`, boolToInt(isActive), id)
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

// DeleteAPIKey removes an API key by id.
func (s *Store) DeleteAPIKey(id string) error {
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id=?`, id)
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
