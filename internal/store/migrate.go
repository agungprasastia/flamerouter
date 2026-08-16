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
		var exists int
		if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE name=?`, name).Scan(&exists); err != nil {
			return err
		}

		if exists > 0 {
			continue
		}

		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}

		for _, stmt := range splitSQL(string(body)) {
			if _, err := tx.Exec(stmt); err != nil {
				if isDuplicateColumn(err) {
					continue
				}

				_ = tx.Rollback()

				return fmt.Errorf("migration %s: %w", name, err)
			}
		}

		if _, err := tx.Exec(`INSERT INTO schema_migrations(name, applied_at) VALUES(?, datetime('now'))`, name); err != nil {
			_ = tx.Rollback()
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}

func splitSQL(body string) []string {
	var out []string

	for _, part := range strings.Split(body, ";") {
		s := strings.TrimSpace(part)
		if s == "" || strings.HasPrefix(s, "--") {
			// drop pure-comment chunks; keep statements that start with SQL
			lines := strings.Split(s, "\n")

			var kept []string

			for _, ln := range lines {
				t := strings.TrimSpace(ln)
				if t == "" || strings.HasPrefix(t, "--") {
					continue
				}

				kept = append(kept, ln)
			}

			s = strings.TrimSpace(strings.Join(kept, "\n"))
		}

		if s != "" {
			out = append(out, s)
		}
	}

	return out
}

func isDuplicateColumn(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	return strings.Contains(msg, "duplicate column")
}
