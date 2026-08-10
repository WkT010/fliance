package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" { dsn = "postgres://nexa:nexa_dev@localhost:5432/nexa?sslmode=disable" }
	db, err := sql.Open("postgres", dsn)
	if err != nil { slog.Error("connect failed", "err", err); os.Exit(1) }
	defer db.Close()
	if err := runMigrations(db); err != nil { slog.Error("migration failed", "err", err); os.Exit(1) }
	slog.Info("migrations complete")
}

func runMigrations(db *sql.DB) error {
	db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	files, _ := filepath.Glob("scripts/migrate/migrations/*.sql"); sort.Strings(files)
	for _, f := range files {
		ver := 0; fmt.Sscanf(filepath.Base(f), "%d", &ver)
		if ver == 0 { continue }
		var applied int
		db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=$1`, ver).Scan(&applied)
		if applied > 0 { slog.Info("skip migration", "file", filepath.Base(f)); continue }
		sqlBytes, _ := os.ReadFile(f)
		for _, stmt := range strings.Split(string(sqlBytes), ";") {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" { continue }
			if _, err := db.Exec(stmt); err != nil { return fmt.Errorf("%s: %w", filepath.Base(f), err) }
		}
		db.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, ver)
		slog.Info("applied migration", "file", filepath.Base(f))
	}
	return nil
}
