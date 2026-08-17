// Package store provides SQLite persistent storage for flamerouter state.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store wraps a persistent SQLite database handle.
type Store struct {
	db *sql.DB
}

// Open initializes and migrates the SQLite database at dataDir.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}

	dsn := filepath.Join(dataDir, "flamerouter.db")

	db, err := sql.Open("sqlite", dsn+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)

	if err := migrate(db); err != nil {
		if clErr := db.Close(); clErr != nil {
			_ = clErr
		}

		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &Store{db: db}, nil
}

// DB returns the underlying sql.DB.
func (s *Store) DB() *sql.DB { return s.db }

// Close closes the underlying SQLite database.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}

	return s.db.Close()
}
