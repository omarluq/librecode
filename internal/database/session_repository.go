// Package database contains database-backed persistence and adapters.
package database

import (
	"context"
	"errors"
	"time"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
)

const deleteSessionMessage = "delete session"

// ChildSessionRequest describes a child session created with durable agent work.
type ChildSessionRequest struct {
	CWD             string
	Name            string
	ParentSessionID string
}

// SessionRepository provides persistence for sessions and tree entries.
type SessionRepository struct {
	sql ksql.Provider
	now func() time.Time
}

// NewSessionRepositoryWithProvider creates a session repository with an explicit SQL provider.
func NewSessionRepositoryWithProvider(provider ksql.Provider) (*SessionRepository, error) {
	if isNilProvider(provider) {
		return nil, nilProviderError()
	}

	return &SessionRepository{sql: provider, now: time.Now}, nil
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

func prepareSession(now time.Time, cwd, name, parentSession string) (*SessionEntity, error) {
	created := &SessionEntity{
		CreatedAt:     now.UTC(),
		UpdatedAt:     now.UTC(),
		ID:            newSessionID(),
		CWD:           cwd,
		Name:          name,
		ParentSession: parentSession,
	}
	if err := validateSessionEntity(created); err != nil {
		return nil, oops.In("database").Code("validate_session").Wrapf(err, "validate session")
	}

	return created, nil
}

func insertSession(ctx context.Context, provider ksql.Provider, session *SessionEntity) error {
	const statement = `
INSERT INTO sessions (id, cwd, name, parent_session, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`

	if _, err := provider.Exec(
		ctx,
		statement,
		session.ID,
		session.CWD,
		session.Name,
		session.ParentSession,
		formatTime(session.CreatedAt),
		formatTime(session.UpdatedAt),
	); err != nil {
		return oops.In("database").Code("create_session").Wrapf(err, "create session")
	}

	return nil
}

// CreateSession creates a new persisted session for a working directory.
func (repository *SessionRepository) CreateSession(
	ctx context.Context,
	cwd string,
	name string,
	parentSession string,
) (*SessionEntity, error) {
	created, err := prepareSession(repository.now(), cwd, name, parentSession)
	if err != nil {
		return nil, err
	}

	if err := insertSession(ctx, repository.sql, created); err != nil {
		return nil, err
	}

	return created, nil
}

// LatestSession returns the newest top-level session for cwd.
func (repository *SessionRepository) LatestSession(ctx context.Context, cwd string) (*SessionEntity, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ? AND parent_session = ''
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

// ListSessions returns top-level sessions for cwd ordered by newest first.
func (repository *SessionRepository) ListSessions(ctx context.Context, cwd string) ([]SessionEntity, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ? AND parent_session = ''
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

// ListChildSessions returns direct child sessions ordered by newest first.
func (repository *SessionRepository) ListChildSessions(
	ctx context.Context,
	parentSessionID string,
) ([]SessionEntity, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE parent_session = ?
ORDER BY updated_at DESC`

	rows := []sessionRow{}
	if err := repository.sql.Query(ctx, &rows, query, parentSessionID); err != nil {
		return nil, oops.In("database").Code("list_child_sessions").Wrapf(err, "query child sessions")
	}

	sessions, err := sessionsFromRows(rows)
	if err != nil {
		return nil, oops.In("database").Code("scan_session").Wrapf(err, "scan child sessions")
	}

	return sessions, nil
}

// DeleteSession removes a session and its entry/message rows.
func (repository *SessionRepository) DeleteSession(ctx context.Context, sessionID string) error {
	if err := repository.sql.Transaction(ctx, func(transaction ksql.Provider) error {
		return deleteSessionRows(ctx, transaction, sessionID)
	}); err != nil {
		return oops.In("database").Code("delete_session").Wrapf(err, deleteSessionMessage)
	}

	return nil
}

func deleteSessionRows(ctx context.Context, transaction ksql.Provider, sessionID string) error {
	if err := deleteOwnedWorkflowChildren(ctx, transaction, sessionID); err != nil {
		return err
	}

	var retained struct {
		Count int `ksql:"count"`
	}
	if err := transaction.QueryOne(ctx, &retained, `SELECT COUNT(*) AS count FROM agent_tasks a
JOIN workflow_agent_tasks wat ON wat.agent_task_id = a.task_id
WHERE a.child_session_id = ?`, sessionID); err != nil {
		return oops.In("database").Code("check_retained_child_session").Wrapf(err, "check retained child session")
	}

	if retained.Count != 0 {
		return oops.In("database").Code("retained_child_session").
			Errorf("child session is retained by a workflow run")
	}

	if _, err := transaction.Exec(ctx,
		`DELETE FROM tasks WHERE id IN (SELECT task_id FROM agent_tasks WHERE child_session_id = ?)`, sessionID,
	); err != nil {
		return oops.In("database").Code("delete_session").Wrapf(err, deleteSessionMessage)
	}

	if err := deleteSessionContent(ctx, transaction, sessionID); err != nil {
		return oops.In("database").Code("delete_session").Wrapf(err, deleteSessionMessage)
	}

	if _, err := transaction.Exec(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return oops.In("database").Code("delete_session").Wrapf(err, deleteSessionMessage)
	}

	return nil
}

func deleteOwnedWorkflowChildren(ctx context.Context, transaction ksql.Provider, ownerSessionID string) error {
	var workflowChildren []struct {
		TaskID         string `ksql:"task_id"`
		ChildSessionID string `ksql:"child_session_id"`
	}
	if err := transaction.Query(ctx, &workflowChildren, `SELECT a.task_id, a.child_session_id
FROM workflow_agent_tasks wat
JOIN agent_tasks a ON a.task_id = wat.agent_task_id
JOIN tasks child ON child.id = a.task_id
JOIN workflow_runs w ON w.task_id = wat.workflow_task_id
JOIN tasks workflow ON workflow.id = w.task_id
WHERE workflow.owner_session_id = ? AND child.owner_session_id = ?`, ownerSessionID, ownerSessionID); err != nil {
		return oops.In("database").Code("delete_workflow_tree").Wrapf(err, "load workflow session tree")
	}

	for _, child := range workflowChildren {
		if err := deleteSessionContent(ctx, transaction, child.ChildSessionID); err != nil {
			return oops.In("database").Code("delete_workflow_tree").Wrapf(err, "delete workflow session tree")
		}

		for _, cleanup := range []struct {
			statement string
			argument  string
		}{
			{statement: `DELETE FROM session_completion_deliveries WHERE task_id = ?`, argument: child.TaskID},
			{statement: `DELETE FROM tasks WHERE id = ?`, argument: child.TaskID},
			{statement: `DELETE FROM sessions WHERE id = ?`, argument: child.ChildSessionID},
		} {
			if _, err := transaction.Exec(ctx, cleanup.statement, cleanup.argument); err != nil {
				return oops.In("database").Code("delete_workflow_tree").Wrapf(err, "delete workflow session tree")
			}
		}
	}

	return nil
}

func deleteSessionContent(ctx context.Context, transaction ksql.Provider, sessionID string) error {
	for _, statement := range []string{
		`DELETE FROM session_messages WHERE session_id = ?`,
		`DELETE FROM session_entries WHERE session_id = ?`,
	} {
		if _, err := transaction.Exec(ctx, statement, sessionID); err != nil {
			return oops.In("database").Code("delete_session_content").Wrapf(err, "delete session content")
		}
	}

	return nil
}
