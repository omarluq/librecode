package database

import (
	"context"
	"errors"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
)

type sessionMessageRow struct {
	ID        string `ksql:"id"`
	SessionID string `ksql:"session_id"`
	EntryID   string `ksql:"entry_id"`
	Sender    string `ksql:"sender"`
	Role      string `ksql:"role"`
	Content   string `ksql:"content"`
	Provider  string `ksql:"provider"`
	Model     string `ksql:"model"`
	CreatedAt string `ksql:"created_at"`
}

func sessionMessageFromRow(row *sessionMessageRow) (*SessionMessageEntity, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}

	return &SessionMessageEntity{
		CreatedAt: createdAt,
		ID:        row.ID,
		SessionID: row.SessionID,
		EntryID:   row.EntryID,
		Sender:    row.Sender,
		Role:      Role(row.Role),
		Content:   row.Content,
		Provider:  row.Provider,
		Model:     row.Model,
	}, nil
}

func sessionMessagesFromRows(rows []sessionMessageRow) ([]SessionMessageEntity, error) {
	return collectSQLRows(rows, sessionMessageFromRow)
}

// Messages returns normalized messages for a session in creation order.
func (repository *SessionRepository) Messages(ctx context.Context, sessionID string) ([]SessionMessageEntity, error) {
	const query = `
SELECT id, session_id, entry_id, sender, role, content, provider, model, created_at
FROM session_messages
WHERE session_id = ?
ORDER BY created_at ASC`

	rows := []sessionMessageRow{}
	if err := repository.sql.Query(ctx, &rows, query, sessionID); err != nil {
		return nil, oops.In("database").Code("list_messages").Wrapf(err, "query session messages")
	}

	messages, err := sessionMessagesFromRows(rows)
	if err != nil {
		return nil, oops.In("database").Code("scan_message").Wrapf(err, "scan session messages")
	}

	return messages, nil
}

// MessageForEntry returns the normalized message for an entry.
func (repository *SessionRepository) MessageForEntry(
	ctx context.Context,
	sessionID string,
	entryID string,
) (*SessionMessageEntity, bool, error) {
	const query = `
SELECT id, session_id, entry_id, sender, role, content, provider, model, created_at
FROM session_messages
WHERE session_id = ? AND entry_id = ?`

	var row sessionMessageRow
	if err := repository.sql.QueryOne(ctx, &row, query, sessionID, entryID); err != nil {
		if errors.Is(err, ksql.ErrRecordNotFound) {
			return nil, false, nil
		}

		return nil, false, oops.In("database").Code("get_message").Wrapf(err, "load session message")
	}

	message, err := sessionMessageFromRow(&row)
	if err != nil {
		return nil, false, oops.In("database").Code("scan_message").Wrapf(err, "scan session message")
	}

	return message, true, nil
}

func (repository *SessionRepository) appendEntryMessage(
	ctx context.Context,
	transaction ksql.Provider,
	entry *EntryEntity,
) error {
	if !entryCarriesMessage(entry) {
		return nil
	}

	message := sessionMessageFromEntry(entry)
	if err := validateSessionMessageEntity(&message); err != nil {
		return oops.In("database").Code("validate_message").Wrapf(err, "validate session message")
	}

	const insertMessage = `
INSERT INTO session_messages (id, session_id, entry_id, sender, role, content, provider, model, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := transaction.Exec(
		ctx,
		insertMessage,
		message.ID,
		message.SessionID,
		message.EntryID,
		message.Sender,
		string(message.Role),
		message.Content,
		message.Provider,
		message.Model,
		formatTime(message.CreatedAt),
	)
	if err != nil {
		return oops.In("database").Code("append_message").Wrapf(err, "append session message")
	}

	return nil
}

func entryCarriesMessage(entry *EntryEntity) bool {
	return entry.Message.Role != ""
}

func sessionMessageFromEntry(entry *EntryEntity) SessionMessageEntity {
	createdAt := entry.Message.Timestamp
	if createdAt.IsZero() {
		createdAt = entry.CreatedAt
	}

	return SessionMessageEntity{
		CreatedAt: createdAt,
		ID:        newEntryID(),
		SessionID: entry.SessionID,
		EntryID:   entry.ID,
		Sender:    senderIdentity(entry),
		Role:      entry.Message.Role,
		Content:   entry.Message.Content,
		Provider:  entry.Message.Provider,
		Model:     entry.Message.Model,
	}
}

func senderIdentity(entry *EntryEntity) string {
	if entry.Message.Role == RoleCustom && entry.CustomType != "" {
		return entry.CustomType
	}

	return string(entry.Message.Role)
}
