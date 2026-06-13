package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/samber/oops"
)

const sqliteBusyTimeoutPragma = "busy_timeout"

// SQLiteOptions contains connection-level SQLite settings.
type SQLiteOptions struct {
	BusyTimeout time.Duration
}

// SQLiteDSN returns a modernc SQLite URI with connection pragmas for librecode's
// multi-process session database. The path may be a filesystem path or an
// existing SQLite URI.
func SQLiteDSN(path string, options SQLiteOptions) string {
	values := sqlitePragmas(options)

	parsed, err := url.Parse(path)
	if err != nil || parsed.Scheme == "" {
		return sqliteFileURI(path, values)
	}

	query := parsed.Query()
	for _, pragma := range values["_pragma"] {
		query.Add("_pragma", pragma)
	}

	parsed.RawQuery = query.Encode()

	return parsed.String()
}

// ConfigureSQLite applies connection-level pragmas that cannot be reliably set
// only through a DSN for every existing database/sql connection.
func ConfigureSQLite(ctx context.Context, connection *sql.DB, options SQLiteOptions) error {
	for _, statement := range sqlitePragmaStatements(options) {
		if _, err := connection.ExecContext(ctx, statement); err != nil {
			return oops.In("database").Code("sqlite_pragma").With("statement", statement).Wrapf(err, "configure sqlite")
		}
	}

	return nil
}

func sqliteFileURI(path string, query url.Values) string {
	uri := &url.URL{Scheme: "file", Path: path}
	uri.RawQuery = query.Encode()

	return uri.String()
}

func sqlitePragmas(options SQLiteOptions) url.Values {
	query := url.Values{}
	for _, pragma := range sqlitePragmaValues(options) {
		query.Add("_pragma", pragma)
	}

	return query
}

func sqlitePragmaValues(options SQLiteOptions) []string {
	busyTimeout := max(int(options.BusyTimeout/time.Millisecond), 0)

	return []string{
		fmt.Sprintf("%s=%s", sqliteBusyTimeoutPragma, strconv.Itoa(busyTimeout)),
		"journal_mode=WAL",
		"synchronous=NORMAL",
		"foreign_keys=ON",
	}
}

func sqlitePragmaStatements(options SQLiteOptions) []string {
	statements := make([]string, 0, len(sqlitePragmaValues(options)))
	for _, pragma := range sqlitePragmaValues(options) {
		statements = append(statements, "PRAGMA "+pragma)
	}

	return statements
}
