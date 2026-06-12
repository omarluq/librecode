package database

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

// MigrationFS returns the embedded migration filesystem rooted at migrations/.
func MigrationFS() (fs.FS, error) {
	migrationRoot, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("database: prepare migrations: %w", err)
	}

	return migrationRoot, nil
}

// NewMigrationProvider returns a goose migration provider for the given database.
func NewMigrationProvider(database *sql.DB, migrationRoot fs.FS) (*goose.Provider, error) {
	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		database,
		migrationRoot,
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return nil, fmt.Errorf("database: create migration provider: %w", err)
	}

	return provider, nil
}

// Migrate applies embedded SQLite schema migrations.
func Migrate(ctx context.Context, database *sql.DB) error {
	migrationRoot, err := MigrationFS()
	if err != nil {
		return err
	}
	provider, err := NewMigrationProvider(database, migrationRoot)
	if err != nil {
		return err
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("database: apply migrations: %w", err)
	}

	return nil
}
