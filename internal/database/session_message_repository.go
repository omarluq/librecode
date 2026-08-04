package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/oops"
	"github.com/vingarcia/ksql"
)

type sessionMessagePartRow struct {
	EntryID  string `ksql:"entry_id"`
	Type     string `ksql:"type"`
	Text     string `ksql:"text"`
	MIMEType string `ksql:"mime_type"`
	Name     string `ksql:"name"`
	Data     []byte `ksql:"data"`
	Width    int    `ksql:"width"`
	Height   int    `ksql:"height"`
}

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
		Model:     row.Model, Parts: nil,
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

	return repository.querySessionMessages(ctx, sessionID, query, "messages")
}

// TranscriptMessages returns displayable normalized messages for a session in creation order.
func (repository *SessionRepository) TranscriptMessages(
	ctx context.Context,
	sessionID string,
) ([]SessionMessageEntity, error) {
	const query = `
SELECT m.id, m.session_id, m.entry_id, m.sender, m.role, m.content, m.provider, m.model, m.created_at
FROM session_messages AS m
JOIN session_entries AS e ON e.id = m.entry_id AND e.session_id = m.session_id
WHERE m.session_id = ? AND e.display = 1
ORDER BY m.created_at ASC`

	return repository.querySessionMessages(ctx, sessionID, query, "transcript_messages")
}

func (repository *SessionRepository) querySessionMessages(
	ctx context.Context,
	sessionID string,
	query string,
	operation string,
) ([]SessionMessageEntity, error) {
	operationLabel := strings.ReplaceAll(operation, "_", " ")

	rows := []sessionMessageRow{}
	if err := repository.sql.Query(ctx, &rows, query, sessionID); err != nil {
		return nil, oops.In("database").Code("list_"+operation).Wrapf(err, "query %s", operationLabel)
	}

	messages, err := sessionMessagesFromRows(rows)
	if err != nil {
		return nil, oops.In("database").Code("scan_"+operation).Wrapf(err, "scan %s", operationLabel)
	}

	if err := repository.hydrateSessionMessages(ctx, sessionID, messages); err != nil {
		return nil, err
	}

	return messages, nil
}

// ContextHasImageParts reports whether an entry or one of its ancestors has an image part.
func (repository *SessionRepository) ContextHasImageParts(
	ctx context.Context,
	sessionID string,
	entryID string,
) (bool, error) {
	const query = `
WITH RECURSIVE chain(id, parent_id) AS (
    SELECT id, parent_id
    FROM session_entries
    WHERE session_id = ? AND id = ?
    UNION ALL
    SELECT parent.id, parent.parent_id
    FROM session_entries AS parent
    JOIN chain AS child ON parent.id = child.parent_id
    WHERE parent.session_id = ?
)
SELECT EXISTS (
    SELECT 1
    FROM session_message_parts AS part
    JOIN chain ON chain.id = part.entry_id
    WHERE part.session_id = ? AND part.type = ?
) AS present`

	var row struct {
		Present int `ksql:"present"`
	}
	if err := repository.sql.QueryOne(
		ctx, &row, query, sessionID, entryID, sessionID, sessionID, string(MessagePartImage),
	); err != nil {
		return false, oops.In("database").Code("context_has_image_parts").
			Wrapf(err, "query context image parts")
	}

	return row.Present != 0, nil
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

	messages := []SessionMessageEntity{*message}
	if err := repository.hydrateSessionMessages(ctx, sessionID, messages); err != nil {
		return nil, false, err
	}

	return &messages[0], true, nil
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

	const insertPart = `
INSERT INTO session_message_parts
    (id, session_id, entry_id, sequence, type, text, mime_type, name, width, height, data)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	for sequence := range message.Parts {
		part := &message.Parts[sequence]
		if _, err := transaction.Exec(ctx, insertPart,
			newEntryID(), message.SessionID, message.EntryID, sequence, string(part.Type),
			part.Text, part.MIMEType, part.Name, part.Width, part.Height, part.Data,
		); err != nil {
			return oops.In("database").Code("append_message_part").Wrapf(err, "append session message part")
		}
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
		Parts:     cloneMessageParts(entry.Message.Parts),
	}
}

func (repository *SessionRepository) hydrateSessionMessages(
	ctx context.Context,
	sessionID string,
	messages []SessionMessageEntity,
) error {
	entryIDs := make([]string, len(messages))
	for index := range messages {
		entryIDs[index] = messages[index].EntryID
	}

	partsByEntry, err := repository.messagePartsForEntries(ctx, sessionID, entryIDs)
	if err != nil {
		return err
	}

	for index := range messages {
		messages[index].Parts = partsOrLegacyText(partsByEntry[messages[index].EntryID], messages[index].Content)
	}

	return nil
}

func (repository *SessionRepository) hydrateEntryMessage(
	ctx context.Context,
	sessionID string,
	entry *EntryEntity,
) error {
	if entry == nil {
		return nil
	}

	entries := []EntryEntity{*entry}
	if err := repository.hydrateEntryMessages(ctx, sessionID, entries); err != nil {
		return err
	}

	*entry = entries[0]

	return nil
}

func (repository *SessionRepository) hydrateEntryMessages(
	ctx context.Context,
	sessionID string,
	entries []EntryEntity,
) error {
	entryIDs := make([]string, 0, len(entries))
	for index := range entries {
		if entryCarriesMessage(&entries[index]) {
			entryIDs = append(entryIDs, entries[index].ID)
		}
	}

	partsByEntry, err := repository.messagePartsForEntries(ctx, sessionID, entryIDs)
	if err != nil {
		return err
	}

	for index := range entries {
		if entryCarriesMessage(&entries[index]) {
			parts := partsByEntry[entries[index].ID]
			entries[index].Message.Parts = partsOrLegacyText(parts, entries[index].Message.Content)
		}
	}

	return nil
}

func (repository *SessionRepository) messagePartsForEntries(
	ctx context.Context,
	sessionID string,
	entryIDs []string,
) (map[string][]MessagePartEntity, error) {
	const entryIDBatchSize = 900

	partsByEntry := make(map[string][]MessagePartEntity, len(entryIDs))
	for start := 0; start < len(entryIDs); start += entryIDBatchSize {
		end := min(start+entryIDBatchSize, len(entryIDs))
		if err := repository.appendMessagePartsBatch(ctx, sessionID, entryIDs[start:end], partsByEntry); err != nil {
			return nil, err
		}
	}

	return partsByEntry, nil
}

func (repository *SessionRepository) appendMessagePartsBatch(
	ctx context.Context,
	sessionID string,
	entryIDs []string,
	partsByEntry map[string][]MessagePartEntity,
) error {
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(entryIDs)), ",")
	query := fmt.Sprintf(`
SELECT entry_id, type, text, data, mime_type, name, width, height
FROM session_message_parts
WHERE session_id = ? AND entry_id IN (%s)
ORDER BY entry_id, sequence`, placeholders)
	args := make([]any, 0, len(entryIDs)+1)

	args = append(args, sessionID)
	for _, entryID := range entryIDs {
		args = append(args, entryID)
	}

	rows := []sessionMessagePartRow{}
	if err := repository.sql.Query(ctx, &rows, query, args...); err != nil {
		return oops.In("database").Code("list_message_parts").Wrapf(err, "query message parts")
	}

	for index := range rows {
		row := &rows[index]
		partsByEntry[row.EntryID] = append(partsByEntry[row.EntryID], MessagePartEntity{
			Type: MessagePartType(row.Type), Text: row.Text, Data: cloneBytes(row.Data),
			MIMEType: row.MIMEType, Name: row.Name, Width: row.Width, Height: row.Height,
		})
	}

	return nil
}

func partsOrLegacyText(parts []MessagePartEntity, content string) []MessagePartEntity {
	if len(parts) == 0 && strings.TrimSpace(content) != "" {
		return []MessagePartEntity{{
			Data: nil, Text: content, MIMEType: "", Name: "", Type: MessagePartText, Width: 0, Height: 0,
		}}
	}

	return cloneMessageParts(parts)
}

func cloneMessageParts(parts []MessagePartEntity) []MessagePartEntity {
	if parts == nil {
		return nil
	}

	cloned := make([]MessagePartEntity, len(parts))
	copy(cloned, parts)

	for index := range cloned {
		cloned[index].Data = cloneBytes(parts[index].Data)
	}

	return cloned
}

func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}

	return append([]byte(nil), data...)
}

func senderIdentity(entry *EntryEntity) string {
	if entry.Message.Role == RoleCustom && entry.CustomType != "" {
		return entry.CustomType
	}

	return string(entry.Message.Role)
}
