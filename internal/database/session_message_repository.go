package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	EntryID    string `ksql:"entry_id"`
	SessionID  string `ksql:"session_id"`
	CustomType string `ksql:"custom_type"`
	Role       string `ksql:"role"`
	Provider   string `ksql:"provider"`
	Model      string `ksql:"model"`
	CreatedAt  string `ksql:"created_at"`
}

func sessionMessageFromRow(row *sessionMessageRow, parts []MessagePartEntity) (*SessionMessageEntity, error) {
	createdAt, err := parseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}

	entry := EntryEntity{
		ID:                         row.EntryID,
		SessionID:                  row.SessionID,
		ParentID:                   nil,
		Type:                       "",
		CustomType:                 row.CustomType,
		DataJSON:                   "",
		Summary:                    "",
		CreatedAt:                  createdAt,
		ToolName:                   "",
		ToolStatus:                 "",
		ToolArgsJSON:               "",
		CompactionFirstKeptEntryID: "",
		BranchFromEntryID:          "",
		TokenEstimate:              0,
		ModelFacing:                false,
		Display:                    false,
		CompactionTokensBefore:     0,
		Message: MessageEntity{
			Timestamp: createdAt,
			Role:      Role(row.Role),
			Content:   "",
			Provider:  row.Provider,
			Model:     row.Model,
			Parts:     nil,
		},
	}
	message := projectSessionMessage(&entry, parts)

	return &message, nil
}

const sessionMessageSelectColumns = `
e.id AS entry_id, e.session_id, e.custom_type, m.role, m.provider, m.model, e.created_at`

// Messages returns normalized messages for a session in deterministic creation order.
func (repository *SessionRepository) Messages(ctx context.Context, sessionID string) ([]SessionMessageEntity, error) {
	query := fmt.Sprintf(`SELECT %s FROM session_entries AS e
JOIN session_messages AS m ON m.entry_id = e.id
WHERE e.session_id = ? ORDER BY e.created_at ASC, e.id ASC`, sessionMessageSelectColumns)

	return repository.querySessionMessages(ctx, query, "messages", sessionID)
}

// TranscriptMessages returns displayable normalized messages in deterministic creation order.
func (repository *SessionRepository) TranscriptMessages(
	ctx context.Context,
	sessionID string,
) ([]SessionMessageEntity, error) {
	query := fmt.Sprintf(`SELECT %s FROM session_entries AS e
JOIN session_messages AS m ON m.entry_id = e.id
WHERE e.session_id = ? AND e.display = 1 ORDER BY e.created_at ASC, e.id ASC`, sessionMessageSelectColumns)

	return repository.querySessionMessages(ctx, query, "transcript_messages", sessionID)
}

// TranscriptMessageTail returns at most limit displayable messages from the end, chronologically.
func (repository *SessionRepository) TranscriptMessageTail(
	ctx context.Context,
	sessionID string,
	limit int,
) ([]SessionMessageEntity, error) {
	if limit <= 0 {
		return []SessionMessageEntity{}, nil
	}

	query := fmt.Sprintf(`SELECT entry_id, session_id, custom_type, role, provider, model, created_at FROM (
SELECT %s FROM session_entries AS e INDEXED BY idx_session_entries_transcript_cursor
JOIN session_messages AS m ON m.entry_id = e.id
WHERE e.session_id = ? AND e.display = 1 ORDER BY e.created_at DESC, e.id DESC LIMIT ?)
ORDER BY created_at ASC, entry_id ASC`, sessionMessageSelectColumns)

	return repository.querySessionMessages(ctx, query, "transcript_message_tail", sessionID, limit)
}

// TranscriptMessagesBefore returns at most limit displayable messages older than the cursor.
func (repository *SessionRepository) TranscriptMessagesBefore(
	ctx context.Context,
	sessionID string,
	beforeCreatedAt time.Time,
	beforeEntryID string,
	limit int,
) ([]SessionMessageEntity, error) {
	if limit <= 0 {
		return []SessionMessageEntity{}, nil
	}

	query := fmt.Sprintf(`SELECT entry_id, session_id, custom_type, role, provider, model, created_at FROM (
SELECT %s FROM session_entries AS e INDEXED BY idx_session_entries_transcript_cursor
JOIN session_messages AS m ON m.entry_id = e.id
WHERE e.session_id = ? AND e.display = 1 AND (e.created_at < ? OR (e.created_at = ? AND e.id < ?))
ORDER BY e.created_at DESC, e.id DESC LIMIT ?) ORDER BY created_at ASC, entry_id ASC`, sessionMessageSelectColumns)
	cursor := formatTime(beforeCreatedAt)

	return repository.querySessionMessages(
		ctx, query, "transcript_messages_before", sessionID, cursor, cursor, beforeEntryID, limit,
	)
}

func (repository *SessionRepository) querySessionMessages(
	ctx context.Context,
	query string,
	operation string,
	args ...any,
) ([]SessionMessageEntity, error) {
	label := strings.ReplaceAll(operation, "_", " ")

	result, err := readOnlyTransactionValue(
		ctx, repository.sql,
		func(provider ksql.Provider) ([]SessionMessageEntity, error) {
			rows := []sessionMessageRow{}
			if err := provider.Query(ctx, &rows, query, args...); err != nil {
				return nil, oops.In("database").Code("list_"+operation).Wrapf(err, "query %s", label)
			}

			ids := make([]string, len(rows))
			for i := range rows {
				ids[i] = rows[i].EntryID
			}

			parts, err := repository.withProvider(provider).messagePartsForEntries(ctx, ids)
			if err != nil {
				return nil, err
			}

			messages := make([]SessionMessageEntity, len(rows))
			for rowIndex := range rows {
				message, scanErr := sessionMessageFromRow(
					&rows[rowIndex],
					parts[rows[rowIndex].EntryID],
				)
				if scanErr != nil {
					return nil, oops.In("database").Code("scan_"+operation).Wrapf(scanErr, "scan %s", label)
				}

				messages[rowIndex] = *message
			}

			return messages, nil
		},
	)
	if err != nil {
		return nil, err
	}

	return result.value, nil
}

// ContextHasImageParts reports whether an entry or one of its ancestors has an image part.
func (repository *SessionRepository) ContextHasImageParts(
	ctx context.Context,
	sessionID string,
	entryID string,
) (bool, error) {
	const query = `WITH RECURSIVE chain(id,parent_id) AS (
SELECT id,parent_id FROM session_entries WHERE session_id=? AND id=? UNION ALL
SELECT p.id,p.parent_id FROM session_entries p JOIN chain c ON p.id=c.parent_id WHERE p.session_id=? )
SELECT EXISTS(SELECT 1 FROM chain JOIN session_messages m ON m.entry_id=chain.id
JOIN session_message_parts part ON part.entry_id=m.entry_id WHERE part.type=?) AS present`

	var row struct {
		Present int `ksql:"present"`
	}
	if err := repository.sql.QueryOne(
		ctx, &row, query, sessionID, entryID, sessionID, string(MessagePartImage),
	); err != nil {
		return false, oops.In("database").Code("context_has_image_parts").Wrapf(err, "query context image parts")
	}

	return row.Present != 0, nil
}

// MessageForEntry returns the normalized message for an entry scoped to its session.
func (repository *SessionRepository) MessageForEntry(
	ctx context.Context,
	sessionID string,
	entryID string,
) (*SessionMessageEntity, bool, error) {
	query := fmt.Sprintf(`SELECT %s FROM session_entries e
JOIN session_messages m ON m.entry_id=e.id
WHERE e.session_id=? AND e.id=?`, sessionMessageSelectColumns)

	type result struct {
		message *SessionMessageEntity
		found   bool
	}

	got, err := readOnlyTransactionValue(ctx, repository.sql, func(provider ksql.Provider) (result, error) {
		var row sessionMessageRow
		if err := provider.QueryOne(ctx, &row, query, sessionID, entryID); err != nil {
			if errors.Is(err, ksql.ErrRecordNotFound) {
				return result{message: nil, found: false}, nil
			}

			return result{}, oops.In("database").Code("get_message").Wrapf(err, "load session message")
		}

		parts, err := repository.withProvider(provider).messagePartsForEntries(ctx, []string{entryID})
		if err != nil {
			return result{}, err
		}

		message, err := sessionMessageFromRow(&row, parts[entryID])
		if err != nil {
			return result{}, oops.In("database").Code("scan_message").Wrapf(err, "scan session message")
		}

		return result{message: message, found: true}, nil
	})
	if err != nil {
		return nil, false, err
	}

	return got.value.message, got.value.found, nil
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

	const insertMessage = `INSERT INTO session_messages (entry_id,role,provider,model) VALUES (?,?,?,?)`
	if _, err := transaction.Exec(
		ctx, insertMessage, message.EntryID, string(message.Role), message.Provider, message.Model,
	); err != nil {
		return oops.In("database").Code("append_message").Wrapf(err, "append session message")
	}

	const insertPart = `INSERT INTO session_message_parts
(entry_id,sequence,type,text,mime_type,name,width,height,data) VALUES (?,?,?,?,?,?,?,?,?)`

	for sequence := range message.Parts {
		part := &message.Parts[sequence]
		if _, err := transaction.Exec(
			ctx, insertPart, message.EntryID, sequence, string(part.Type), part.Text,
			part.MIMEType, part.Name, part.Width, part.Height, part.Data,
		); err != nil {
			return oops.In("database").Code("append_message_part").Wrapf(err, "append session message part")
		}
	}

	return nil
}

func entryCarriesMessage(entry *EntryEntity) bool { return entry.Message.Role != "" }
func sessionMessageFromEntry(entry *EntryEntity) SessionMessageEntity {
	return projectSessionMessage(entry, entry.Message.Parts)
}

func (repository *SessionRepository) withProvider(provider ksql.Provider) *SessionRepository {
	snapshot := *repository
	snapshot.sql = provider

	return &snapshot
}

func (repository *SessionRepository) hydrateEntryMessage(ctx context.Context, entry *EntryEntity) error {
	if entry == nil {
		return nil
	}

	entries := []EntryEntity{*entry}
	if err := repository.hydrateEntryMessages(ctx, entries); err != nil {
		return err
	}

	*entry = entries[0]

	return nil
}

func (repository *SessionRepository) hydrateEntryMessages(ctx context.Context, entries []EntryEntity) error {
	if len(entries) == 0 {
		return nil
	}

	ids := make([]string, len(entries))

	byID := make(map[string]int, len(entries))
	for i := range entries {
		ids[i] = entries[i].ID
		byID[entries[i].ID] = i
	}

	if err := repository.hydrateMessageEnvelopes(ctx, entries, ids, byID); err != nil {
		return err
	}

	messageIDs := make([]string, 0, len(entries))
	for i := range entries {
		if entryCarriesMessage(&entries[i]) {
			messageIDs = append(messageIDs, entries[i].ID)
		}
	}

	parts, err := repository.messagePartsForEntries(ctx, messageIDs)
	if err != nil {
		return err
	}

	for i := range entries {
		if entryCarriesMessage(&entries[i]) {
			entries[i].Message.Parts = cloneMessageParts(parts[entries[i].ID])
			entries[i].Message.Content = messagePartsText(entries[i].Message.Parts)
		}
	}

	return nil
}

func (repository *SessionRepository) hydrateMessageEnvelopes(
	ctx context.Context,
	entries []EntryEntity,
	ids []string,
	byID map[string]int,
) error {
	const batchSize = 900
	for start := 0; start < len(ids); start += batchSize {
		end := min(start+batchSize, len(ids))
		placeholders := strings.TrimSuffix(strings.Repeat("?,", end-start), ",")
		query := fmt.Sprintf(
			`SELECT entry_id,role,provider,model FROM session_messages WHERE entry_id IN (%s)`,
			placeholders,
		)

		args := make([]any, end-start)
		for index, id := range ids[start:end] {
			args[index] = id
		}

		var rows []struct {
			EntryID  string `ksql:"entry_id"`
			Role     string `ksql:"role"`
			Provider string `ksql:"provider"`
			Model    string `ksql:"model"`
		}
		if err := repository.sql.Query(ctx, &rows, query, args...); err != nil {
			return oops.In("database").Code("list_message_envelopes").Wrapf(err, "query message envelopes")
		}

		for _, row := range rows {
			entry := &entries[byID[row.EntryID]]
			entry.Message.Timestamp = entry.CreatedAt
			entry.Message.Role = Role(row.Role)
			entry.Message.Provider = row.Provider
			entry.Message.Model = row.Model
		}
	}

	return nil
}

func (repository *SessionRepository) messagePartsForEntries(
	ctx context.Context,
	entryIDs []string,
) (map[string][]MessagePartEntity, error) {
	const batchSize = 900

	parts := make(map[string][]MessagePartEntity, len(entryIDs))
	for start := 0; start < len(entryIDs); start += batchSize {
		end := min(start+batchSize, len(entryIDs))
		if err := repository.appendMessagePartsBatch(ctx, entryIDs[start:end], parts); err != nil {
			return nil, err
		}
	}

	return parts, nil
}
func (repository *SessionRepository) appendMessagePartsBatch(
	ctx context.Context,
	ids []string,
	parts map[string][]MessagePartEntity,
) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	query := fmt.Sprintf(`SELECT entry_id,type,text,data,mime_type,name,width,height
FROM session_message_parts WHERE entry_id IN (%s) ORDER BY entry_id,sequence`, placeholders)

	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows := []sessionMessagePartRow{}
	if err := repository.sql.Query(ctx, &rows, query, args...); err != nil {
		return oops.In("database").Code("list_message_parts").Wrapf(err, "query message parts")
	}

	for _, row := range rows {
		parts[row.EntryID] = append(parts[row.EntryID], MessagePartEntity{
			Type:     MessagePartType(row.Type),
			Text:     row.Text,
			MIMEType: row.MIMEType,
			Name:     row.Name,
			Data:     cloneBytes(row.Data),
			Width:    row.Width,
			Height:   row.Height,
		})
	}

	return nil
}
func cloneMessageParts(parts []MessagePartEntity) []MessagePartEntity {
	if parts == nil {
		return nil
	}

	cloned := make([]MessagePartEntity, len(parts))
	copy(cloned, parts)

	for i := range cloned {
		cloned[i].Data = cloneBytes(parts[i].Data)
	}

	return cloned
}
func cloneBytes(data []byte) []byte {
	if data == nil {
		return nil
	}

	return append([]byte(nil), data...)
}
