package di

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/do/v2"
	"github.com/samber/oops"
	_ "modernc.org/sqlite" // register the SQLite database driver.

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/startupprofile"
)

const (
	sqliteDriverName = "sqlite"
	databaseDirMode  = 0o700
)

// DatabaseService owns the session database connection and schema lifecycle.
type DatabaseService struct {
	DB          *sql.DB
	Sessions    *database.SessionRepository
	Documents   *database.DocumentRepository
	Tasks       *database.TaskRepository
	AgentTasks  *database.AgentTaskRepository
	Workflows   *database.WorkflowRepository
	ToolTasks   *database.ToolTaskRepository
	Completions *database.CompletionRepository
	path        string
}

// NewDatabaseService opens the session database and applies embedded migrations.
func NewDatabaseService(injector do.Injector) (*DatabaseService, error) {
	cfg, err := databaseConfig(injector)
	if err != nil {
		return nil, err
	}

	ctx, err := applicationContext(injector)
	if err != nil {
		return nil, err
	}

	if contextErr := ctx.Err(); contextErr != nil {
		return nil, oops.In("database").Code("startup_canceled").Wrapf(contextErr, "initialize database")
	}

	databasePath, err := resolveDatabasePath(cfg)
	if err != nil {
		return nil, err
	}

	mkdirErr := os.MkdirAll(filepath.Dir(databasePath), databaseDirMode)
	if mkdirErr != nil {
		return nil, oops.In("database").Code("mkdir").With("path", databasePath).Wrapf(mkdirErr, "create database dir")
	}

	finishDatabase := startupprofile.FromContext(ctx).Span("database")
	connection, err := openSQLiteDatabase(ctx, databasePath, cfg.Database)

	finishDatabase()

	if err != nil {
		return nil, err
	}

	service, err := newDatabaseRepositories(connection, databasePath)
	if err != nil {
		return nil, err
	}

	return service, nil
}

func newDatabaseRepositories(connection *sql.DB, databasePath string) (*DatabaseService, error) {
	repositories, err := database.NewRepositories(connection)
	if err != nil {
		return nil, closeAfterSetupError(
			connection, "close_after_repositories", "repositories", databasePath, err,
		)
	}

	return &DatabaseService{
		DB: connection, Sessions: repositories.Sessions, Documents: repositories.Documents,
		Tasks: repositories.Tasks, AgentTasks: repositories.AgentTasks,
		ToolTasks: repositories.ToolTasks, Workflows: repositories.Workflows,
		Completions: repositories.Completions, path: databasePath,
	}, nil
}

func databaseConfig(injector do.Injector) (*config.Config, error) {
	configService, err := do.Invoke[*ConfigService](injector)
	if err != nil {
		return nil, err
	}

	return configService.Get(), nil
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
	if err := ctx.Err(); err != nil {
		return serviceError(err, "shutdown database")
	}

	if err := service.DB.Close(); err != nil {
		return oops.In("database").Code("close").Wrapf(err, "close database")
	}

	return nil
}

func openSQLiteDatabase(ctx context.Context, databasePath string, cfg config.DatabaseConfig) (*sql.DB, error) {
	dsn := database.SQLiteDSN(databasePath, database.SQLiteOptions{BusyTimeout: cfg.BusyTimeout})

	connection, err := sql.Open(sqliteDriverName, dsn)
	if err != nil {
		return nil, oops.In("database").Code("open").With("path", databasePath).Wrapf(err, "open database")
	}

	connection.SetMaxOpenConns(cfg.MaxOpenConns)
	connection.SetMaxIdleConns(cfg.MaxIdleConns)
	connection.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := setupSQLiteDatabase(ctx, connection, databasePath, cfg); err != nil {
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
