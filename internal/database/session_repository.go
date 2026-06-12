// Package database contains database-backed persistence and adapters.
package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
	ksqlite "github.com/vingarcia/ksql/adapters/modernc-ksqlite"
)

// SessionRepository provides persistence for sessions and tree entries.
type SessionRepository struct {
	sql ksql.Provider
	now func() time.Time
}

// NewSessionRepository creates a session repository.
func NewSessionRepository(connection *sql.DB) *SessionRepository {
	sqlProvider, err := ksqlite.NewFromSQLDB(connection)
	if err != nil {
		panic(err)
	}

	return NewSessionRepositoryWithProvider(sqlProvider)
}

// NewSessionRepositoryWithProvider creates a session repository with an explicit SQL provider.
func NewSessionRepositoryWithProvider(sqlProvider ksql.Provider) *SessionRepository {
	return &SessionRepository{
		sql: sqlProvider,
		now: time.Now,
	}
}

type sessionRow struct {
	ID            string `ksql:"id"`
	CWD           string `ksql:"cwd"`
	Name          string `ksql:"name"`
	ParentSession string `ksql:"parent_session"`
	CreatedAt     string `ksql:"created_at"`
	UpdatedAt     string `ksql:"updated_at"`
}

func sessionFromRow(row *sessionRow) (*SessionEntity, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTime(row.UpdatedAt)
	if err != nil {
		return nil, err
	}

	return &SessionEntity{
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		ID:            row.ID,
		CWD:           row.CWD,
		Name:          row.Name,
		ParentSession: row.ParentSession,
	}, nil
}

func sessionsFromRows(rows []sessionRow) ([]SessionEntity, error) {
	return collectSQLRows(rows, sessionFromRow)
}

func newSessionID() string {
	return newUUIDv7()
}

// CreateSession creates a new persisted session for a working directory.
func (repository *SessionRepository) CreateSession(
	ctx context.Context,
	cwd string,
	name string,
	parentSession string,
) (*SessionEntity, error) {
	timestamp := repository.now().UTC()
	createdSession := SessionEntity{
		CreatedAt:     timestamp,
		UpdatedAt:     timestamp,
		ID:            newSessionID(),
		CWD:           cwd,
		Name:          name,
		ParentSession: parentSession,
	}

	const statement = `
INSERT INTO sessions (id, cwd, name, parent_session, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`
	if err := validateSessionEntity(&createdSession); err != nil {
		return nil, oops.In("database").Code("validate_session").Wrapf(err, "validate session")
	}

	if _, err := repository.sql.Exec(
		ctx,
		statement,
		createdSession.ID,
		createdSession.CWD,
		createdSession.Name,
		createdSession.ParentSession,
		formatTime(createdSession.CreatedAt),
		formatTime(createdSession.UpdatedAt),
	); err != nil {
		return nil, oops.In("database").Code("create_session").Wrapf(err, "create session")
	}

	return &createdSession, nil
}

// LatestSession returns the newest session for cwd.
func (repository *SessionRepository) LatestSession(ctx context.Context, cwd string) (*SessionEntity, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ?
ORDER BY updated_at DESC
LIMIT 1`

	return repository.loadSession(ctx, query, "latest_session", "load latest session", cwd)
}

// GetSession loads a session by id.
func (repository *SessionRepository) GetSession(ctx context.Context, sessionID string) (*SessionEntity, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE id = ?`

	return repository.loadSession(ctx, query, "get_session", "load session", sessionID)
}

func (repository *SessionRepository) loadSession(
	ctx context.Context,
	query string,
	code string,
	message string,
	args ...any,
) (*SessionEntity, bool, error) {
	var row sessionRow
	if err := repository.sql.QueryOne(ctx, &row, query, args...); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code(code).Wrapf(err, "%s", message)
	}

	foundSession, err := sessionFromRow(&row)
	if err != nil {
		return nil, false, oops.In("database").Code("scan_session").Wrapf(err, "scan session")
	}

	return foundSession, true, nil
}

// ListSessions returns sessions for cwd ordered by newest first.
func (repository *SessionRepository) ListSessions(ctx context.Context, cwd string) ([]SessionEntity, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ?
ORDER BY updated_at DESC`

	rows := []sessionRow{}
	if err := repository.sql.Query(ctx, &rows, query, cwd); err != nil {
		return nil, oops.In("database").Code("list_sessions").Wrapf(err, "query sessions")
	}

	sessions, err := sessionsFromRows(rows)
	if err != nil {
		return nil, oops.In("database").Code("scan_session").Wrapf(err, "scan sessions")
	}

	return sessions, nil
}

// DeleteSession removes a session and its entry/message rows.
func (repository *SessionRepository) DeleteSession(ctx context.Context, sessionID string) error {
	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		return deleteSessionRows(ctx, transaction, sessionID)
	}); err != nil {
		return oops.In("database").Code("delete_session").Wrapf(err, "delete session")
	}

	return nil
}

func deleteSessionRows(ctx context.Context, transaction ksql.Provider, sessionID string) error {
	statements := []string{
		`DELETE FROM session_messages WHERE session_id = ?`,
		`DELETE FROM session_entries WHERE session_id = ?`,
		`DELETE FROM sessions WHERE id = ?`,
	}
	for _, statement := range statements {
		if _, err := transaction.Exec(ctx, statement, sessionID); err != nil {
			return oops.In("database").Code("delete_session").Wrapf(err, "delete session")
		}
	}

	return nil
}
