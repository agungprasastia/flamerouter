// Package store provides SQLite persistent storage for flamerouter state.
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}

	var names []string

	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}

	sort.Strings(names)

	for _, name := range names {
		if err := applyMigration(db, name); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(db *sql.DB, name string) error {
	var exists int
	if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE name=?`, name).Scan(&exists); err != nil {
		return err
	}

	if exists > 0 {
		return nil
	}

	body, err := migrationFS.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	defer func() {
		//nolint:errcheck // safe no-op if committed
		_ = tx.Rollback()
	}()

	if err := executeMigrationStatements(tx, name, string(body)); err != nil {
		return err
	}

	return tx.Commit()
}

func executeMigrationStatements(tx *sql.Tx, name, sqlContent string) error {
	for _, stmt := range splitSQL(sqlContent) {
		if _, err := tx.Exec(stmt); err != nil {
			if isDuplicateColumn(err) {
				continue
			}

			if rbErr := tx.Rollback(); rbErr != nil {
				_ = rbErr
			}

			return fmt.Errorf("migration %s: %w", name, err)
		}
	}

	if _, err := tx.Exec(`INSERT INTO schema_migrations(name, applied_at) VALUES(?, datetime('now'))`, name); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			_ = rbErr
		}

		return err
	}

	return nil
}

func splitSQL(body string) []string {
	var out []string

	for _, part := range strings.Split(body, ";") {
		s := strings.TrimSpace(part)
		if s == "" || strings.HasPrefix(s, "--") {
			s = filterComments(s)
		}

		if s != "" {
			out = append(out, s)
		}
	}

	return out
}

func filterComments(s string) string {
	lines := strings.Split(s, "\n")

	kept := make([]string, 0, len(lines))

	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "--") {
			continue
		}

		kept = append(kept, ln)
	}

	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "duplicate column")
}
