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
	_ "modernc.org/sqlite" // register the SQLite database driver.

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
)

const sqliteDriverName = "sqlite"

// DatabaseService owns the session database connection and schema lifecycle.
type DatabaseService struct {
	DB    *sql.DB
	Store *database.SessionStore
	path  string
}

// NewDatabaseService opens the session database and applies embedded migrations.
func NewDatabaseService(injector do.Injector) (*DatabaseService, error) {
	cfg := do.MustInvoke[*ConfigService](injector).Get()
	databasePath, err := resolveDatabasePath(cfg)
	if err != nil {
		return nil, err
	}

	mkdirErr := os.MkdirAll(filepath.Dir(databasePath), 0o700)
	if mkdirErr != nil {
		return nil, oops.In("database").Code("mkdir").With("path", databasePath).Wrapf(mkdirErr, "create database dir")
	}

	connection, err := sql.Open(sqliteDriverName, databasePath)
	if err != nil {
		return nil, oops.In("database").Code("open").With("path", databasePath).Wrapf(err, "open database")
	}

	connection.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	connection.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	connection.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)

	if err := connection.PingContext(context.Background()); err != nil {
		closeErr := connection.Close()
		if closeErr != nil {
			return nil, oops.In("database").Code("close_after_ping").Wrapf(closeErr, "close failed database")
		}
		return nil, oops.In("database").Code("ping").With("path", databasePath).Wrapf(err, "ping database")
	}

	if cfg.Database.ApplyMigrations {
		if err := database.Migrate(context.Background(), connection); err != nil {
			closeErr := connection.Close()
			if closeErr != nil {
				return nil, oops.In("database").Code("close_after_migrate").Wrapf(closeErr, "close failed database")
			}
			return nil, oops.In("database").Code("migrate").With("path", databasePath).Wrapf(err, "migrate database")
		}
	}

	return &DatabaseService{
		DB:    connection,
		Store: database.NewSessionStore(connection),
		path:  databasePath,
	}, nil
}

// Path returns the resolved session database path.
func (service *DatabaseService) Path() string {
	return service.path
}

// HealthCheck verifies the database connection is alive.
func (service *DatabaseService) HealthCheck(ctx context.Context) error {
	return service.DB.PingContext(ctx)
}

// Shutdown closes the database connection.
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
