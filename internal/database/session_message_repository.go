package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/samber/oops"
)

// Messages returns normalized messages for a session in creation order.
func (repository *SessionRepository) Messages(ctx context.Context, sessionID string) ([]SessionMessageEntity, error) {
	const query = `
SELECT id, session_id, entry_id, sender, role, content, provider, model, created_at
FROM session_messages
WHERE session_id = ?
ORDER BY created_at ASC`

	rows, err := repository.connection.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, oops.In("database").Code("list_messages").Wrapf(err, "query session messages")
	}

	return collectRows(rows, scanSessionMessage, &rowErrorInfo{
		scanCode:  "scan_message",
		scanMsg:   "scan session message",
		iterCode:  "iterate_messages",
		iterMsg:   "iterate session messages",
		closeCode: "close_messages",
		closeMsg:  "close message rows",
	})
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

	message, err := scanSessionMessage(repository.connection.QueryRowContext(ctx, query, sessionID, entryID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("get_message").Wrapf(err, "load session message")
	}

	return message, true, nil
}

func (repository *SessionRepository) appendEntryMessage(
	ctx context.Context,
	transaction *sql.Tx,
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
	_, err := transaction.ExecContext(
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

func scanSessionMessage(scanner rowScanner) (*SessionMessageEntity, error) {
	var createdAtRaw string
	var role string
	message := SessionMessageEntity{
		CreatedAt: time.Time{},
		ID:        "",
		SessionID: "",
		EntryID:   "",
		Sender:    "",
		Role:      "",
		Content:   "",
		Provider:  "",
		Model:     "",
	}
	if err := scanner.Scan(
		&message.ID,
		&message.SessionID,
		&message.EntryID,
		&message.Sender,
		&role,
		&message.Content,
		&message.Provider,
		&message.Model,
		&createdAtRaw,
	); err != nil {
		return nil, err
	}
	createdAt, err := parseTime(createdAtRaw)
	if err != nil {
		return nil, err
	}
	message.CreatedAt = createdAt
	message.Role = Role(role)

	return &message, nil
}
