package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

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
	return newMigrationProvider(database, migrationRoot, nil)
}

func newMigrationProvider(database *sql.DB, migrationRoot fs.FS, logger *slog.Logger) (*goose.Provider, error) {
	options := []goose.ProviderOption{goose.WithDisableGlobalRegistry(true)}
	if logger != nil {
		options = append(options, goose.WithVerbose(true), goose.WithSlog(logger))
	}

	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		database,
		migrationRoot,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("database: create migration provider: %w", err)
	}

	return provider, nil
}

// Migrate applies embedded SQLite schema migrations.
func Migrate(ctx context.Context, database *sql.DB) error {
	return migrate(ctx, database, nil)
}

// MigrateWithLogger applies migrations with statement-level progress logging.
func MigrateWithLogger(ctx context.Context, database *sql.DB, logger *slog.Logger) error {
	return migrate(ctx, database, logger)
}

func migrate(ctx context.Context, database *sql.DB, logger *slog.Logger) error {
	// Migration 23 rebuilds the ownership graph and requires foreign keys to be
	// active before Goose opens its transaction (the pragma is ineffective in a
	// transaction). Production connections already set this in their DSN; keep
	// direct migration callers equally safe.
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("database: enable migration foreign keys: %w", err)
	}

	migrationRoot, err := MigrationFS()
	if err != nil {
		return err
	}

	provider, err := newMigrationProvider(database, migrationRoot, logger)
	if err != nil {
		return err
	}

	startedAt := time.Now()

	if logger != nil {
		logger.InfoContext(ctx, "applying database migrations")
	}

	results, err := provider.Up(ctx)
	if err != nil {
		if logger != nil {
			logger.ErrorContext(ctx, "database migration failed", "duration", time.Since(startedAt), "error", err)
		}

		return fmt.Errorf("database: apply migrations: %w", err)
	}

	if logger != nil {
		logger.InfoContext(
			ctx, "database migrations complete", "duration", time.Since(startedAt), "applied", len(results),
		)
	}

	return nil
}
