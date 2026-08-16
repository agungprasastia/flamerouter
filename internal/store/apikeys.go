package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"time"
)

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)

	return hex.EncodeToString(b)
}

func (s *Store) CreateAPIKey(name, keyID, keyHash, machineID string) (string, error) {
	id := newID()
	_, err := s.db.Exec(
		`INSERT INTO api_keys(id, name, key_id, key_hash, machine_id, is_active, created_at)
		 VALUES(?,?,?,?,?,1,?)`,
		id, name, keyID, keyHash, machineID, time.Now().UTC().Format(time.RFC3339),
	)

	return id, err
}

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

type APIKey struct {
	ID        string
	Name      string
	KeyID     string
	MachineID string
	CreatedAt string
	IsActive  bool
}

func (s *Store) ListAPIKeys() ([]APIKey, error) {
	rows, err := s.db.Query(
		`SELECT id, name, key_id, COALESCE(machine_id,''), is_active, created_at FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

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

func (s *Store) UpdateAPIKey(id string, isActive bool) error {
	res, err := s.db.Exec(`UPDATE api_keys SET is_active=? WHERE id=?`, boolToInt(isActive), id)
	if err != nil {
		return err
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (s *Store) DeleteAPIKey(id string) error {
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id=?`, id)
	if err != nil {
		return err
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}

	return nil
}
