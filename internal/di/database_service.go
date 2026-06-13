package di

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/do/v2"
	"github.com/samber/oops"
	_ "modernc.org/sqlite" // register the SQLite database driver.

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"
)

const (
	sqliteDriverName = "sqlite"
	databaseDirMode  = 0o700
)

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

	mkdirErr := os.MkdirAll(filepath.Dir(databasePath), databaseDirMode)
	if mkdirErr != nil {
		return nil, oops.In("database").Code("mkdir").With("path", databasePath).Wrapf(mkdirErr, "create database dir")
	}

	connection, err := openSQLiteDatabase(databasePath, cfg.Database)
	if err != nil {
		return nil, err
	}

	sqlProvider, err := ksqlite.NewFromSQLDB(connection)
	if err != nil {
		return nil, closeAfterSetupError(connection, "close_after_sql_provider", "sql_provider", databasePath, err)
	}

	return &DatabaseService{
		DB:        connection,
		Sessions:  database.NewSessionRepositoryWithProvider(sqlProvider),
		Documents: database.NewDocumentRepositoryWithProvider(sqlProvider),
		path:      databasePath,
	}, nil
}

// Path returns the resolved session database path.
func (service *DatabaseService) Path() string {
	return service.path
}

// HealthCheck verifies the database connection is alive.
func (service *DatabaseService) HealthCheck(ctx context.Context) error {
	return serviceError(service.DB.PingContext(ctx), "ping database")
}

// Shutdown closes the database connection.
func (service *DatabaseService) Shutdown(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return serviceError(ctx.Err(), "shutdown database")
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
	setupErr := oops.In("database").Code(code).With("path", databasePath).Wrapf(err, "%s database", code)
	if closeErr := connection.Close(); closeErr != nil {
		return oops.In("database").Code(closeCode).With("path", databasePath).Wrapf(
			errors.Join(setupErr, closeErr),
			"close failed after %s database setup at %s",
			code,
			databasePath,
		)
	}

	return setupErr
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
