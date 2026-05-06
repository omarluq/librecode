package session

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies embedded SQLite schema migrations.
func Migrate(ctx context.Context, database *sql.DB) error {
	migrationRoot, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("session: prepare migrations: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, database, migrationRoot, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return fmt.Errorf("session: create migration provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("session: apply migrations: %w", err)
	}

	return nil
}
