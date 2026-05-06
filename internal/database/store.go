// Package database contains database-backed persistence and adapters.
package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/samber/oops"
)

const entrySelectColumns = `
id, session_id, parent_id, entry_type, role, content,
provider, model, custom_type, data_json, summary, created_at`

// SessionStore provides persistence for sessions and tree entries.
type SessionStore struct {
	connection *sql.DB
	now        func() time.Time
}

// NewSessionStore creates a session store.
func NewSessionStore(connection *sql.DB) *SessionStore {
	return &SessionStore{
		connection: connection,
		now:        time.Now,
	}
}

// CreateSession creates a new persisted session for a working directory.
func (store *SessionStore) CreateSession(ctx context.Context, cwd, name, parentSession string) (*SessionEntity, error) {
	timestamp := store.now().UTC()
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
	if _, err := store.connection.ExecContext(
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
func (store *SessionStore) LatestSession(ctx context.Context, cwd string) (*SessionEntity, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ?
ORDER BY updated_at DESC
LIMIT 1`

	foundSession, err := scanSession(store.connection.QueryRowContext(ctx, query, cwd))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("latest_session").Wrapf(err, "load latest session")
	}

	return foundSession, true, nil
}

// GetSession loads a session by id.
func (store *SessionStore) GetSession(ctx context.Context, sessionID string) (*SessionEntity, bool, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE id = ?`

	foundSession, err := scanSession(store.connection.QueryRowContext(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("get_session").Wrapf(err, "load session")
	}

	return foundSession, true, nil
}

// ListSessions returns sessions for cwd ordered by newest first.
func (store *SessionStore) ListSessions(ctx context.Context, cwd string) ([]SessionEntity, error) {
	const query = `
SELECT id, cwd, name, parent_session, created_at, updated_at
FROM sessions
WHERE cwd = ?
ORDER BY updated_at DESC`

	rows, err := store.connection.QueryContext(ctx, query, cwd)
	if err != nil {
		return nil, oops.In("database").Code("list_sessions").Wrapf(err, "query sessions")
	}

	return collectRows(rows, scanSession, &rowErrorInfo{
		scanCode:  "scan_session",
		scanMsg:   "scan session",
		iterCode:  "iterate_sessions",
		iterMsg:   "iterate sessions",
		closeCode: "close_sessions",
		closeMsg:  "close session rows",
	})
}

// LeafEntry returns the newest appended entry for a session.
func (store *SessionStore) LeafEntry(ctx context.Context, sessionID string) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ?
ORDER BY created_at DESC
LIMIT 1`, entrySelectColumns)

	entry, err := scanEntry(store.connection.QueryRowContext(ctx, query, sessionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("leaf_entry").Wrapf(err, "load leaf entry")
	}

	return entry, true, nil
}

// Entries returns all entries for a session in append order.
func (store *SessionStore) Entries(ctx context.Context, sessionID string) ([]EntryEntity, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ?
ORDER BY created_at ASC`, entrySelectColumns)

	rows, err := store.connection.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, oops.In("database").Code("list_entries").Wrapf(err, "query session entries")
	}

	return collectRows(rows, scanEntry, &rowErrorInfo{
		scanCode:  "scan_entry",
		scanMsg:   "scan session entry",
		iterCode:  "iterate_entries",
		iterMsg:   "iterate session entries",
		closeCode: "close_entries",
		closeMsg:  "close entry rows",
	})
}

// Entry loads one entry by id.
func (store *SessionStore) Entry(ctx context.Context, sessionID, entryID string) (*EntryEntity, bool, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ? AND id = ?`, entrySelectColumns)

	entry, err := scanEntry(store.connection.QueryRowContext(ctx, query, sessionID, entryID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, oops.In("database").Code("get_entry").Wrapf(err, "load entry")
	}

	return entry, true, nil
}

// Children returns direct child entries for a parent id.
func (store *SessionStore) Children(ctx context.Context, sessionID string, parentID *string) ([]EntryEntity, error) {
	query := fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ? AND parent_id IS NULL
ORDER BY created_at ASC`, entrySelectColumns)
	args := []any{sessionID}
	if parentID != nil {
		query = fmt.Sprintf(`
SELECT %s
FROM session_entries
WHERE session_id = ? AND parent_id = ?
ORDER BY created_at ASC`, entrySelectColumns)
		args = append(args, *parentID)
	}

	rows, err := store.connection.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, oops.In("database").Code("list_children").Wrapf(err, "query child entries")
	}

	return collectRows(rows, scanEntry, &rowErrorInfo{
		scanCode:  "scan_child",
		scanMsg:   "scan child entry",
		iterCode:  "iterate_children",
		iterMsg:   "iterate child entries",
		closeCode: "close_children",
		closeMsg:  "close child rows",
	})
}

// Branch returns the path from root to the requested entry or current leaf.
func (store *SessionStore) Branch(ctx context.Context, sessionID, entryID string) ([]EntryEntity, error) {
	entries, err := store.Entries(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return []EntryEntity{}, nil
	}

	entryByID := make(map[string]EntryEntity, len(entries))
	for index := range entries {
		entryByID[entries[index].ID] = entries[index]
	}

	currentID := entryID
	if currentID == "" {
		currentID = entries[len(entries)-1].ID
	}

	branch := []EntryEntity{}
	seen := map[string]bool{}
	for currentID != "" {
		if seen[currentID] {
			return nil, fmt.Errorf("database: session tree contains cycle at %s", currentID)
		}
		seen[currentID] = true

		entry, ok := entryByID[currentID]
		if !ok {
			return nil, fmt.Errorf("database: entry %s not found", currentID)
		}
		branch = append(branch, entry)
		if entry.ParentID == nil {
			break
		}
		currentID = *entry.ParentID
	}

	for left, right := 0, len(branch)-1; left < right; left, right = left+1, right-1 {
		branch[left], branch[right] = branch[right], branch[left]
	}

	return branch, nil
}

// Tree returns the full session entry tree.
func (store *SessionStore) Tree(ctx context.Context, sessionID string) ([]TreeNodeEntity, error) {
	entries, err := store.Entries(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	childrenByParent := map[string][]EntryEntity{}
	for index := range entries {
		parentID := ""
		if entries[index].ParentID != nil {
			parentID = *entries[index].ParentID
		}
		childrenByParent[parentID] = append(childrenByParent[parentID], entries[index])
	}
	for parentID := range childrenByParent {
		sort.Slice(childrenByParent[parentID], func(leftIndex, rightIndex int) bool {
			return childrenByParent[parentID][leftIndex].CreatedAt.Before(
				childrenByParent[parentID][rightIndex].CreatedAt,
			)
		})
	}

	var build func(parentID string) []TreeNodeEntity
	build = func(parentID string) []TreeNodeEntity {
		children := childrenByParent[parentID]
		nodes := make([]TreeNodeEntity, 0, len(children))
		for index := range children {
			nodes = append(nodes, TreeNodeEntity{
				Entry:    children[index],
				Children: build(children[index].ID),
			})
		}

		return nodes
	}

	return build(""), nil
}

// Label returns the latest label value for a target entry.
func (store *SessionStore) Label(
	ctx context.Context,
	sessionID string,
	targetID string,
) (label string, found bool, err error) {
	entries, err := store.Entries(ctx, sessionID)
	if err != nil {
		return "", false, err
	}

	for index := range entries {
		if entries[index].Type != EntryTypeLabel {
			continue
		}
		data, err := dataFromEntry(&entries[index])
		if err != nil {
			return "", false, err
		}
		if data.TargetID != targetID {
			continue
		}
		if data.Label == nil {
			label = ""
			found = false
			continue
		}
		label = *data.Label
		found = true
	}

	return label, found, nil
}

// BuildContext reconstructs model-facing context from the active branch.
func (store *SessionStore) BuildContext(ctx context.Context, sessionID, leafID string) (*SessionContextEntity, error) {
	branch, err := store.Branch(ctx, sessionID, leafID)
	if err != nil {
		return nil, err
	}

	contextEntity := SessionContextEntity{
		Messages:      []MessageEntity{},
		Provider:      "",
		Model:         "",
		ThinkingLevel: "",
	}
	for index := range branch {
		if err := applyEntryToContext(&contextEntity, &branch[index]); err != nil {
			return nil, err
		}
	}

	return &contextEntity, nil
}

// AppendMessage appends a message as a child of the current leaf or provided parent.
func (store *SessionStore) AppendMessage(
	ctx context.Context,
	sessionID string,
	parentID *string,
	message *MessageEntity,
) (*EntryEntity, error) {
	entry := EntryEntity{
		Message:    *message,
		CreatedAt:  store.now().UTC(),
		ParentID:   parentID,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       EntryTypeMessage,
		CustomType: "",
		DataJSON:   "{}",
		Summary:    "",
	}

	if err := store.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendCustom appends extension state that does not participate in prompt context.
func (store *SessionStore) AppendCustom(
	ctx context.Context,
	sessionID string,
	customType string,
	dataJSON string,
) (*EntryEntity, error) {
	return store.AppendCustomEntry(ctx, sessionID, nil, customType, dataJSON)
}

// AppendCustomEntry appends extension state with an explicit tree parent.
func (store *SessionStore) AppendCustomEntry(
	ctx context.Context,
	sessionID string,
	parentID *string,
	customType string,
	dataJSON string,
) (*EntryEntity, error) {
	timestamp := store.now().UTC()
	entry := EntryEntity{
		Message: MessageEntity{
			Timestamp: timestamp,
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
		},
		CreatedAt:  timestamp,
		ParentID:   parentID,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       EntryTypeCustom,
		CustomType: customType,
		DataJSON:   normalizeDataJSON(dataJSON),
		Summary:    "",
	}

	if err := store.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendCustomMessage appends extension context that participates in session context.
func (store *SessionStore) AppendCustomMessage(
	ctx context.Context,
	sessionID string,
	parentID *string,
	customType string,
	content string,
	display bool,
	details map[string]any,
) (*EntryEntity, error) {
	dataJSON, err := dataJSONFromEntity(&EntryDataEntity{
		Details:          details,
		Display:          &display,
		FromHook:         false,
		FirstKeptEntryID: "",
		FromID:           "",
		Label:            nil,
		Name:             "",
		TargetID:         "",
		ThinkingLevel:    "",
		TokensBefore:     0,
	})
	if err != nil {
		return nil, err
	}

	timestamp := store.now().UTC()
	entry := EntryEntity{
		Message: MessageEntity{
			Timestamp: timestamp,
			Role:      RoleCustom,
			Content:   content,
			Provider:  "",
			Model:     "",
		},
		CreatedAt:  timestamp,
		ParentID:   parentID,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       EntryTypeCustomMessage,
		CustomType: customType,
		DataJSON:   dataJSON,
		Summary:    "",
	}

	if err := store.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendModelChange records a provider/model switch.
func (store *SessionStore) AppendModelChange(
	ctx context.Context,
	sessionID string,
	parentID *string,
	provider string,
	model string,
) (*EntryEntity, error) {
	timestamp := store.now().UTC()
	entry := EntryEntity{
		Message: MessageEntity{
			Timestamp: timestamp,
			Role:      "",
			Content:   "",
			Provider:  provider,
			Model:     model,
		},
		CreatedAt:  timestamp,
		ParentID:   parentID,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       EntryTypeModelChange,
		CustomType: "",
		DataJSON:   "{}",
		Summary:    "",
	}

	if err := store.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendThinkingLevelChange records a reasoning/thinking level switch.
func (store *SessionStore) AppendThinkingLevelChange(
	ctx context.Context,
	sessionID string,
	parentID *string,
	thinkingLevel string,
) (*EntryEntity, error) {
	dataJSON, err := dataJSONFromEntity(&EntryDataEntity{
		Details:          nil,
		Display:          nil,
		FromHook:         false,
		FirstKeptEntryID: "",
		FromID:           "",
		Label:            nil,
		Name:             "",
		TargetID:         "",
		ThinkingLevel:    thinkingLevel,
		TokensBefore:     0,
	})
	if err != nil {
		return nil, err
	}

	timestamp := store.now().UTC()
	entry := EntryEntity{
		Message: MessageEntity{
			Timestamp: timestamp,
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
		},
		CreatedAt:  timestamp,
		ParentID:   parentID,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       EntryTypeThinkingLevelChange,
		CustomType: "",
		DataJSON:   dataJSON,
		Summary:    "",
	}

	if err := store.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendCompaction records a summary for compacted context.
func (store *SessionStore) AppendCompaction(
	ctx context.Context,
	sessionID string,
	parentID *string,
	summary string,
	firstKeptEntryID string,
	tokensBefore int,
	details map[string]any,
	fromHook bool,
) (*EntryEntity, error) {
	dataJSON, err := dataJSONFromEntity(&EntryDataEntity{
		Details:          details,
		Display:          nil,
		FromHook:         fromHook,
		FirstKeptEntryID: firstKeptEntryID,
		FromID:           "",
		Label:            nil,
		Name:             "",
		TargetID:         "",
		ThinkingLevel:    "",
		TokensBefore:     tokensBefore,
	})
	if err != nil {
		return nil, err
	}

	return store.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeCompaction, summary, dataJSON)
}

// AppendBranchSummary records summary context from an abandoned branch.
func (store *SessionStore) AppendBranchSummary(
	ctx context.Context,
	sessionID string,
	parentID *string,
	fromID string,
	summary string,
	details map[string]any,
	fromHook bool,
) (*EntryEntity, error) {
	dataJSON, err := dataJSONFromEntity(&EntryDataEntity{
		Details:          details,
		Display:          nil,
		FromHook:         fromHook,
		FirstKeptEntryID: "",
		FromID:           fromID,
		Label:            nil,
		Name:             "",
		TargetID:         "",
		ThinkingLevel:    "",
		TokensBefore:     0,
	})
	if err != nil {
		return nil, err
	}

	return store.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeBranchSummary, summary, dataJSON)
}

// AppendLabelChange sets or clears a label for a target entry.
func (store *SessionStore) AppendLabelChange(
	ctx context.Context,
	sessionID string,
	parentID *string,
	targetID string,
	label *string,
) (*EntryEntity, error) {
	dataJSON, err := dataJSONFromEntity(&EntryDataEntity{
		Details:          nil,
		Display:          nil,
		FromHook:         false,
		FirstKeptEntryID: "",
		FromID:           "",
		Label:            label,
		Name:             "",
		TargetID:         targetID,
		ThinkingLevel:    "",
		TokensBefore:     0,
	})
	if err != nil {
		return nil, err
	}

	return store.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeLabel, "", dataJSON)
}

// AppendSessionInfo records a session display name and updates the session row.
func (store *SessionStore) AppendSessionInfo(
	ctx context.Context,
	sessionID string,
	parentID *string,
	name string,
) (*EntryEntity, error) {
	dataJSON, err := dataJSONFromEntity(&EntryDataEntity{
		Details:          nil,
		Display:          nil,
		FromHook:         false,
		FirstKeptEntryID: "",
		FromID:           "",
		Label:            nil,
		Name:             name,
		TargetID:         "",
		ThinkingLevel:    "",
		TokensBefore:     0,
	})
	if err != nil {
		return nil, err
	}

	entry, err := store.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeSessionInfo, "", dataJSON)
	if err != nil {
		return nil, err
	}

	const statement = `UPDATE sessions SET name = ? WHERE id = ?`
	if _, err := store.connection.ExecContext(ctx, statement, name, sessionID); err != nil {
		return nil, oops.In("database").Code("set_session_name").Wrapf(err, "set session name")
	}

	return entry, nil
}

func (store *SessionStore) appendSummaryEntry(
	ctx context.Context,
	sessionID string,
	parentID *string,
	entryType EntryType,
	summary string,
	dataJSON string,
) (*EntryEntity, error) {
	timestamp := store.now().UTC()
	entry := EntryEntity{
		Message: MessageEntity{
			Timestamp: timestamp,
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
		},
		CreatedAt:  timestamp,
		ParentID:   parentID,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       entryType,
		CustomType: "",
		DataJSON:   normalizeDataJSON(dataJSON),
		Summary:    summary,
	}

	if err := store.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (store *SessionStore) appendEntry(ctx context.Context, entry *EntryEntity) error {
	const insertEntry = `
INSERT INTO session_entries (
    id, session_id, parent_id, entry_type, role, content,
    provider, model, custom_type, data_json, summary, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	transaction, err := store.connection.BeginTx(ctx, nil)
	if err != nil {
		return oops.In("database").Code("begin_append").Wrapf(err, "begin append entry")
	}

	if _, err := transaction.ExecContext(
		ctx,
		insertEntry,
		entry.ID,
		entry.SessionID,
		entry.ParentID,
		string(entry.Type),
		string(entry.Message.Role),
		entry.Message.Content,
		entry.Message.Provider,
		entry.Message.Model,
		entry.CustomType,
		entry.DataJSON,
		entry.Summary,
		formatTime(entry.CreatedAt),
	); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return oops.In("database").Code("append_entry_rollback").Wrapf(rollbackErr, "rollback append entry")
		}
		return oops.In("database").Code("append_entry").Wrapf(err, "append session entry")
	}

	const touchSession = `UPDATE sessions SET updated_at = ? WHERE id = ?`
	if _, err := transaction.ExecContext(ctx, touchSession, formatTime(entry.CreatedAt), entry.SessionID); err != nil {
		rollbackErr := transaction.Rollback()
		if rollbackErr != nil {
			return oops.In("database").Code("touch_session_rollback").Wrapf(rollbackErr, "rollback touch session")
		}
		return oops.In("database").Code("touch_session").Wrapf(err, "touch session")
	}

	if err := transaction.Commit(); err != nil {
		return oops.In("database").Code("commit_append").Wrapf(err, "commit append entry")
	}

	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type rowErrorInfo struct {
	scanCode  string
	scanMsg   string
	iterCode  string
	iterMsg   string
	closeCode string
	closeMsg  string
}

func collectRows[T any](rows *sql.Rows, scan func(rowScanner) (*T, error), errorInfo *rowErrorInfo) (
	items []T,
	err error,
) {
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = oops.In("database").Code(errorInfo.closeCode).Wrapf(closeErr, "%s", errorInfo.closeMsg)
		}
	}()

	items = []T{}
	for rows.Next() {
		item, scanErr := scan(rows)
		if scanErr != nil {
			return nil, oops.In("database").Code(errorInfo.scanCode).Wrapf(scanErr, "%s", errorInfo.scanMsg)
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.In("database").Code(errorInfo.iterCode).Wrapf(err, "%s", errorInfo.iterMsg)
	}

	return items, nil
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

func scanEntry(scanner rowScanner) (*EntryEntity, error) {
	var parentID sql.NullString
	var createdAt string
	var entryType string
	var role string
	entry := EntryEntity{
		Message: MessageEntity{
			Timestamp: time.Time{},
			Role:      "",
			Content:   "",
			Provider:  "",
			Model:     "",
		},
		CreatedAt:  time.Time{},
		ParentID:   nil,
		ID:         "",
		SessionID:  "",
		Type:       "",
		CustomType: "",
		DataJSON:   "",
		Summary:    "",
	}

	if err := scanner.Scan(
		&entry.ID,
		&entry.SessionID,
		&parentID,
		&entryType,
		&role,
		&entry.Message.Content,
		&entry.Message.Provider,
		&entry.Message.Model,
		&entry.CustomType,
		&entry.DataJSON,
		&entry.Summary,
		&createdAt,
	); err != nil {
		return nil, err
	}

	if parentID.Valid {
		entry.ParentID = &parentID.String
	}

	parsedCreatedAt, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	entry.Type = EntryType(entryType)
	entry.Message.Role = Role(role)
	entry.Message.Timestamp = parsedCreatedAt
	entry.CreatedAt = parsedCreatedAt

	return &entry, nil
}

func newEntryID() string {
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		return uuid.NewString()[:8]
	}

	return hex.EncodeToString(buffer)
}

func normalizeDataJSON(dataJSON string) string {
	if dataJSON == "" {
		return "{}"
	}

	return dataJSON
}

func dataJSONFromEntity(data *EntryDataEntity) (string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("database: encode entry data: %w", err)
	}

	return string(encoded), nil
}

func dataFromEntry(entry *EntryEntity) (EntryDataEntity, error) {
	data := EntryDataEntity{
		Details:          nil,
		Display:          nil,
		FromHook:         false,
		FirstKeptEntryID: "",
		FromID:           "",
		Label:            nil,
		Name:             "",
		TargetID:         "",
		ThinkingLevel:    "",
		TokensBefore:     0,
	}
	if normalizeDataJSON(entry.DataJSON) == "{}" {
		return data, nil
	}
	if err := json.Unmarshal([]byte(entry.DataJSON), &data); err != nil {
		return data, fmt.Errorf("database: decode entry data: %w", err)
	}

	return data, nil
}

func applyEntryToContext(contextEntity *SessionContextEntity, entry *EntryEntity) error {
	switch entry.Type {
	case EntryTypeMessage:
		contextEntity.Messages = append(contextEntity.Messages, entry.Message)
	case EntryTypeCustomMessage:
		contextEntity.Messages = append(contextEntity.Messages, entry.Message)
	case EntryTypeBranchSummary:
		contextEntity.Messages = append(contextEntity.Messages, MessageEntity{
			Timestamp: entry.CreatedAt,
			Role:      RoleBranchSummary,
			Content:   entry.Summary,
			Provider:  "",
			Model:     "",
		})
	case EntryTypeCompaction:
		contextEntity.Messages = []MessageEntity{{
			Timestamp: entry.CreatedAt,
			Role:      RoleCompactionSummary,
			Content:   entry.Summary,
			Provider:  "",
			Model:     "",
		}}
	case EntryTypeModelChange:
		contextEntity.Provider = entry.Message.Provider
		contextEntity.Model = entry.Message.Model
	case EntryTypeThinkingLevelChange:
		data, err := dataFromEntry(entry)
		if err != nil {
			return err
		}
		contextEntity.ThinkingLevel = data.ThinkingLevel
	case EntryTypeCustom, EntryTypeLabel, EntryTypeSessionInfo:
		return nil
	}

	return nil
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("database: parse timestamp: %w", err)
	}

	return parsed, nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
