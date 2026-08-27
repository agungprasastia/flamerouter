// Package store provides SQLite persistent storage for flamerouter state.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Connection describes a provider connection credential and state.
type Connection struct {
	ProviderSpecificData map[string]any
	RateLimitedUntil     string
	LastError            string
	Name                 string
	LastUsedAt           string `json:"last_used_at"`
	BaseURL              string
	APIKey               string
	AuthType             string
	TestStatus           string
	ExpiresAt            string
	RefreshToken         string
	AccessToken          string
	ID                   string
	Provider             string
	ConsecutiveUseCount  int `json:"consecutive_use_count"`
	Priority             int
	IsActive             bool
}

func scanConnection(rows interface {
	Scan(dest ...any) error
},
) (Connection, error) {
	var c Connection

	var psdJSON string

	var active int

	err := rows.Scan(&c.ID, &c.Provider, &c.AuthType, &c.Name, &c.Priority, &active,
		&c.APIKey, &c.AccessToken, &c.RefreshToken, &c.ExpiresAt,
		&c.TestStatus, &c.LastError, &c.RateLimitedUntil, &psdJSON, &c.BaseURL,
		&c.ConsecutiveUseCount, &c.LastUsedAt)
	if err != nil {
		return c, err
	}

	c.IsActive = active != 0
	if err := json.Unmarshal([]byte(psdJSON), &c.ProviderSpecificData); err != nil {
		c.ProviderSpecificData = nil
	}

	return c, nil
}

const connectionSelect = `SELECT id, provider, auth_type, name, priority, is_active,
		        COALESCE(api_key,''), COALESCE(access_token,''), COALESCE(refresh_token,''),
		        COALESCE(expires_at,''), COALESCE(test_status,''), COALESCE(last_error,''),
		        COALESCE(rate_limited_until,''), COALESCE(provider_specific_data,'{}'),
		        COALESCE(base_url,''),
		        COALESCE(consecutive_use_count,0), COALESCE(last_used_at,'')
		 FROM provider_connections`

// ListActiveByProvider returns all active connections for a given provider.
func (s *Store) ListActiveByProvider(provider string) ([]Connection, error) {
	rows, err := s.db.Query(connectionSelect+` WHERE provider=? AND is_active=1 ORDER BY priority DESC`, provider)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []Connection

	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// ListConnectionsByProvider returns all connections (active or inactive) for a provider.
func (s *Store) ListConnectionsByProvider(provider string) ([]Connection, error) {
	rows, err := s.db.Query(connectionSelect+` WHERE provider=? ORDER BY priority DESC`, provider)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []Connection

	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// ListAllConnections returns all connections across all providers.
func (s *Store) ListAllConnections() ([]Connection, error) {
	rows, err := s.db.Query(connectionSelect + ` ORDER BY priority DESC, provider ASC`)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	var out []Connection

	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}

// GetConnection returns a connection by ID or sql.ErrNoRows if not found.
func (s *Store) GetConnection(id string) (*Connection, error) {
	row := s.db.QueryRow(connectionSelect+` WHERE id=?`, id)

	c, err := scanConnection(row)
	if err != nil {
		return nil, err
	}

	return &c, nil
}

// GetConnectionsByIDs returns connections mapped by ID for a list of IDs.
func (s *Store) GetConnectionsByIDs(ids []string) (map[string]Connection, error) {
	if len(ids) == 0 {
		return map[string]Connection{}, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := connectionSelect + ` WHERE id IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}

	defer func() {
		if clErr := rows.Close(); clErr != nil {
			_ = clErr
		}
	}()

	out := make(map[string]Connection, len(ids))
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}

		out[c.ID] = c
	}

	return out, rows.Err()
}

// CreateConnection inserts a new provider connection.
func (s *Store) CreateConnection(provider, authType, name, apiKey, baseURL string) (string, error) {
	id := newID()
	_, err := s.db.Exec(
		`INSERT INTO provider_connections(id, provider, auth_type, name, priority, is_active, test_status, api_key, base_url, created_at)
		 VALUES(?,?,?,?,0,1,'active',?,?,?)`,
		id, provider, authType, name, apiKey, baseURL, time.Now().UTC().Format(time.RFC3339),
	)

	return id, err
}

// CreateOAuthConnection inserts a connection with tokens + optional PSD.
func (s *Store) CreateOAuthConnection(provider, authType, name, accessToken, refreshToken, expiresAt string, psd map[string]any) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("store database not initialized")
	}

	id := newID()
	psdJSON := "{}"

	if psd != nil {
		if b, err := json.Marshal(psd); err == nil {
			psdJSON = string(b)
		}
	}

	_, err := s.db.Exec(
		`INSERT INTO provider_connections(id, provider, auth_type, name, priority, is_active, test_status, access_token, refresh_token, expires_at, provider_specific_data, created_at)
		 VALUES(?,?,?,?,0,1,'active',?,?,?,?,?)`,
		id, provider, authType, name, accessToken, refreshToken, expiresAt, psdJSON, time.Now().UTC().Format(time.RFC3339),
	)

	return id, err
}

// MarkConnectionUnavailable sets the rate limited until timestamp for a connection.
func (s *Store) MarkConnectionUnavailable(connID string, cooldownMs int64) error {
	until := GetUnavailableUntil(cooldownMs)
	_, err := s.db.Exec(
		`UPDATE provider_connections SET rate_limited_until=? WHERE id=?`,
		until, connID,
	)

	return err
}

// ClearConnectionError resets error and rate limit status for a connection.
func (s *Store) ClearConnectionError(connID string) error {
	_, err := s.db.Exec(
		`UPDATE provider_connections SET rate_limited_until='', last_error='' WHERE id=?`,
		connID,
	)

	return err
}

// UpdateConnectionTokens updates OAuth token credentials for a connection.
func (s *Store) UpdateConnectionTokens(connID, accessToken, refreshToken, expiresAt string) error {
	_, err := s.db.Exec(
		`UPDATE provider_connections SET access_token=?, refresh_token=?, expires_at=? WHERE id=?`,
		accessToken, refreshToken, expiresAt, connID,
	)

	return err
}

// UpdateConnectionPSD updates provider specific data for a connection.
func (s *Store) UpdateConnectionPSD(connID string, psdJSON string) error {
	_, err := s.db.Exec(
		`UPDATE provider_connections SET provider_specific_data=? WHERE id=?`,
		psdJSON, connID,
	)

	return err
}

// InsertUsage records a token usage entry.
func (s *Store) InsertUsage(provider, model string, prompt, completion int, connectionID string) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_entries(provider, model, prompt_tokens, completion_tokens, connection_id, created_at)
		 VALUES(?,?,?,?,?,datetime('now'))`,
		provider, model, prompt, completion, connectionID,
	)

	return err
}

// UpdateConnection updates connection settings.
func (s *Store) UpdateConnection(id string, isActive bool, name string, priority int, baseURL string) error {
	_, err := s.db.Exec(
		`UPDATE provider_connections SET is_active=?, name=?, priority=?, base_url=? WHERE id=?`,
		isActive, name, priority, baseURL, id,
	)

	return err
}

// DeleteConnection deletes a connection by id.
func (s *Store) DeleteConnection(id string) error {
	res, err := s.db.Exec(`DELETE FROM provider_connections WHERE id=?`, id)
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

// GetUnavailableUntil computes ISO timestamp for a cooldown duration.
func GetUnavailableUntil(cooldownMs int64) string {
	return time.Now().Add(time.Duration(cooldownMs) * time.Millisecond).UTC().Format(time.RFC3339)
}
