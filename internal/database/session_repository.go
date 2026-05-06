// Package database contains database-backed persistence and adapters.
package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/samber/oops"
)

// SessionRepository provides persistence for sessions and tree entries.
type SessionRepository struct {
	connection *sql.DB
	now        func() time.Time
}

// NewSessionRepository creates a session repository.
func NewSessionRepository(connection *sql.DB) *SessionRepository {
	return &SessionRepository{
		connection: connection,
		now:        time.Now,
	}
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
		ID:            uuid.NewString(),
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

	if _, err := repository.connection.ExecContext(
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

	foundSession, err := scanSession(repository.connection.QueryRowContext(ctx, query, cwd))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("latest_session").Wrapf(err, "load latest session")
	}

	return foundSession, true, nil
}

// GetSession loads a session by id.
func (repository *SessionRepository) GetSession(ctx context.Context, sessionID string) (*SessionEntity, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE id = ?`

	foundSession, err := scanSession(repository.connection.QueryRowContext(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("get_session").Wrapf(err, "load session")
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

	rows, err := repository.connection.QueryContext(ctx, query, cwd)
	if err != nil {
		return nil, oops.In("database").Code("list_sessions").Wrapf(err, "query sessions")
	}

	return collectRows(rows, scanSession, sessionRowsErrorInfo())
}

// DeleteSession removes a session and its entry/message rows.
func (repository *SessionRepository) DeleteSession(ctx context.Context, sessionID string) error {
	transaction, err := repository.connection.BeginTx(ctx, nil)
	if err != nil {
		return oops.In("database").Code("begin_delete_session").Wrapf(err, "begin delete session")
	}
	if err := deleteSessionRows(ctx, transaction, sessionID); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return oops.In("database").Code("delete_session_rollback").Wrapf(rollbackErr, "rollback delete session")
		}

		return err
	}
	if err := transaction.Commit(); err != nil {
		return oops.In("database").Code("commit_delete_session").Wrapf(err, "commit delete session")
	}

	return nil
}

func deleteSessionRows(ctx context.Context, transaction *sql.Tx, sessionID string) error {
	statements := []string{
		`DELETE FROM session_messages WHERE session_id = ?`,
		`DELETE FROM session_entries WHERE session_id = ?`,
		`DELETE FROM sessions WHERE id = ?`,
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement, sessionID); err != nil {
			return oops.In("database").Code("delete_session").Wrapf(err, "delete session")
		}
	}

	return nil
}

func sessionRowsErrorInfo() *rowErrorInfo {
	return &rowErrorInfo{
		scanCode:  "scan_session",
		scanMsg:   "scan session",
		iterCode:  "iterate_sessions",
		iterMsg:   "iterate sessions",
		closeCode: "close_sessions",
		closeMsg:  "close session rows",
	}
}

func scanSession(scanner rowScanner) (*SessionEntity, error) {
	var createdAt string
	var updatedAt string
	foundSession := SessionEntity{
		CreatedAt:     time.Time{},
		UpdatedAt:     time.Time{},
		ID:            "",
		CWD:           "",
		Name:          "",
		ParentSession: "",
	}

	if err := scanner.Scan(
		&foundSession.ID,
		&foundSession.CWD,
		&foundSession.Name,
		&foundSession.ParentSession,
		&createdAt,
		&updatedAt,
	); err != nil {
		return nil, err
	}

	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	parsedUpdatedAt, err := parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	foundSession.CreatedAt = parsedCreatedAt
	foundSession.UpdatedAt = parsedUpdatedAt

	return &foundSession, nil
}
