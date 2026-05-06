package di

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/do/v2"
	"github.com/samber/oops"
	_ "modernc.org/sqlite"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/session"
)

const sqliteDriverName = "sqlite"

// DatabaseService owns the SQLite database connection and schema lifecycle.
type DatabaseService struct {
	DB    *sql.DB
	Store *session.Store
	path  string
}

// NewDatabaseService opens SQLite and applies embedded goose migrations.
func NewDatabaseService(injector do.Injector) (*DatabaseService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()
	databasePath, err := resolveDatabasePath(cfg)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return nil, oops.In("database").Code("mkdir").With("path", databasePath).Wrapf(err, "create database dir")
	}

	database, err := sql.Open(sqliteDriverName, databasePath)
	if err != nil {
		return nil, oops.In("database").Code("open").With("path", databasePath).Wrapf(err, "open sqlite")
	}

	database.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	database.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	database.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	if err := database.PingContext(context.Background()); err != nil {
		closeErr := database.Close()
		if closeErr != nil {
			return nil, oops.In("database").Code("close_after_ping").Wrapf(closeErr, "close failed database")
		}
		return nil, oops.In("database").Code("ping").With("path", databasePath).Wrapf(err, "ping sqlite")
	}

	if cfg.Database.ApplyMigrations {
		if err := session.Migrate(context.Background(), database); err != nil {
			closeErr := database.Close()
			if closeErr != nil {
				return nil, oops.In("database").Code("close_after_migrate").Wrapf(closeErr, "close failed database")
			}
			return nil, oops.In("database").Code("migrate").With("path", databasePath).Wrapf(err, "migrate sqlite")
		}
	}

	return &DatabaseService{
		DB:    database,
		Store: session.NewStore(database),
		path:  databasePath,
	}, nil
}

// Path returns the resolved SQLite database path.
func (service *DatabaseService) Path() string {
	return service.path
}

// HealthCheck verifies the SQLite connection is alive.
func (service *DatabaseService) HealthCheck(ctx context.Context) error {
	return service.DB.PingContext(ctx)
}

// Shutdown closes the SQLite connection.
func (service *DatabaseService) Shutdown(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		if err := service.DB.Close(); err != nil {
			return fmt.Errorf("database: close: %w", err)
		}

		return nil
	}
}

func resolveDatabasePath(cfg *config.Config) (string, error) {
	if cfg.Database.Path == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", oops.In("database").Code("home_dir").Wrapf(err, "resolve home dir")
		}

		return filepath.Join(homeDir, ".local", "state", "librecode", "sessions.db"), nil
	}

	if strings.HasPrefix(cfg.Database.Path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", oops.In("database").Code("home_dir").Wrapf(err, "resolve home dir")
		}

		return filepath.Join(homeDir, strings.TrimPrefix(cfg.Database.Path, "~/")), nil
	}

	return cfg.Database.Path, nil
}
