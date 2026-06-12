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
	DB        *sql.DB
	Sessions  *database.SessionRepository
	Documents *database.DocumentRepository
	path      string
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

	connection, err := openSQLiteDatabase(databasePath, cfg.Database)
	if err != nil {
		return nil, err
	}

	return &DatabaseService{
		DB:        connection,
		Sessions:  database.NewSessionRepository(connection),
		Documents: database.NewDocumentRepository(connection),
		path:      databasePath,
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

func openSQLiteDatabase(databasePath string, cfg config.DatabaseConfig) (*sql.DB, error) {
	dsn := database.SQLiteDSN(databasePath, database.SQLiteOptions{BusyTimeout: cfg.BusyTimeout})
	connection, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, oops.In("database").Code("open").With("path", databasePath).Wrapf(err, "open database")
	}

	connection.SetMaxOpenConns(cfg.MaxOpenConns)
	connection.SetMaxIdleConns(cfg.MaxIdleConns)
	connection.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	setupCtx := context.Background()
	if err := setupSQLiteDatabase(setupCtx, connection, databasePath, cfg); err != nil {
		return nil, err
	}

	return connection, nil
}

func setupSQLiteDatabase(
	ctx context.Context,
	connection *sql.DB,
	databasePath string,
	cfg config.DatabaseConfig,
) error {
	if err := connection.PingContext(ctx); err != nil {
		return closeAfterSetupError(connection, "close_after_ping", "ping", databasePath, err)
	}

	options := database.SQLiteOptions{BusyTimeout: cfg.BusyTimeout}
	if err := database.ConfigureSQLite(ctx, connection, options); err != nil {
		return closeAfterSetupError(connection, "close_after_configure", "configure", databasePath, err)
	}

	if cfg.ApplyMigrations {
		if err := database.Migrate(ctx, connection); err != nil {
			return closeAfterSetupError(connection, "close_after_migrate", "migrate", databasePath, err)
		}
	}

	return nil
}

func closeAfterSetupError(connection *sql.DB, closeCode, code, databasePath string, err error) error {
	if closeErr := connection.Close(); closeErr != nil {
		return oops.In("database").Code(closeCode).Wrapf(closeErr, "close failed database")
	}

	return oops.In("database").Code(code).With("path", databasePath).Wrapf(err, "%s database", code)
}

func resolveDatabasePath(cfg *config.Config) (string, error) {
	if cfg.Database.Path == "" {
		databasePath, err := defaultDatabasePath()
		if err != nil {
			return "", err
		}

		return databasePath, nil
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

func defaultDatabasePath() (string, error) {
	projectPath, err := projectDataPath("librecode.db")
	if err == nil && fileExists(projectPath) {
		return projectPath, nil
	}

	globalPath, err := userDataPath("librecode.db")
	if err != nil {
		return "", err
	}

	return globalPath, nil
}
