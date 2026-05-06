package database

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/samber/oops"
)

// Branch returns the path from root to the requested entry or current leaf.
func (repository *SessionRepository) Branch(ctx context.Context, sessionID, entryID string) ([]EntryEntity, error) {
	entries, err := repository.Entries(ctx, sessionID)
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
func (repository *SessionRepository) Tree(ctx context.Context, sessionID string) ([]TreeNodeEntity, error) {
	entries, err := repository.Entries(ctx, sessionID)
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
func (repository *SessionRepository) Label(
	ctx context.Context,
	sessionID string,
	targetID string,
) (label string, found bool, err error) {
	entries, err := repository.Entries(ctx, sessionID)
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
func (repository *SessionRepository) BuildContext(
	ctx context.Context,
	sessionID string,
	leafID string,
) (*SessionContextEntity, error) {
	branch, err := repository.Branch(ctx, sessionID, leafID)
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
func (repository *SessionRepository) AppendMessage(
	ctx context.Context,
	sessionID string,
	parentID *string,
	message *MessageEntity,
) (*EntryEntity, error) {
	entry := EntryEntity{
		Message:    *message,
		CreatedAt:  repository.now().UTC(),
		ParentID:   parentID,
		ID:         newEntryID(),
		SessionID:  sessionID,
		Type:       EntryTypeMessage,
		CustomType: "",
		DataJSON:   "{}",
		Summary:    "",
	}

	if err := repository.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendCustom appends extension state that does not participate in prompt context.
func (repository *SessionRepository) AppendCustom(
	ctx context.Context,
	sessionID string,
	customType string,
	dataJSON string,
) (*EntryEntity, error) {
	return repository.AppendCustomEntry(ctx, sessionID, nil, customType, dataJSON)
}

// AppendCustomEntry appends extension state with an explicit tree parent.
func (repository *SessionRepository) AppendCustomEntry(
	ctx context.Context,
	sessionID string,
	parentID *string,
	customType string,
	dataJSON string,
) (*EntryEntity, error) {
	timestamp := repository.now().UTC()
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

	if err := repository.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendCustomMessage appends extension context that participates in session context.
func (repository *SessionRepository) AppendCustomMessage(
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

	timestamp := repository.now().UTC()
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

	if err := repository.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendModelChange records a provider/model switch.
func (repository *SessionRepository) AppendModelChange(
	ctx context.Context,
	sessionID string,
	parentID *string,
	provider string,
	model string,
) (*EntryEntity, error) {
	timestamp := repository.now().UTC()
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

	if err := repository.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendThinkingLevelChange records a reasoning/thinking level switch.
func (repository *SessionRepository) AppendThinkingLevelChange(
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

	timestamp := repository.now().UTC()
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

	if err := repository.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

// AppendCompaction records a summary for compacted context.
func (repository *SessionRepository) AppendCompaction(
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

	return repository.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeCompaction, summary, dataJSON)
}

// AppendBranchSummary records summary context from an abandoned branch.
func (repository *SessionRepository) AppendBranchSummary(
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

	return repository.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeBranchSummary, summary, dataJSON)
}

// AppendLabelChange sets or clears a label for a target entry.
func (repository *SessionRepository) AppendLabelChange(
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

	return repository.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeLabel, "", dataJSON)
}

// AppendSessionInfo records a session display name and updates the session row.
func (repository *SessionRepository) AppendSessionInfo(
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

	entry, err := repository.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeSessionInfo, "", dataJSON)
	if err != nil {
		return nil, err
	}

	const statement = `UPDATE sessions SET name = ? WHERE id = ?`
	if _, err := repository.connection.ExecContext(ctx, statement, name, sessionID); err != nil {
		return nil, oops.In("database").Code("set_session_name").Wrapf(err, "set session name")
	}

	return entry, nil
}

func (repository *SessionRepository) appendSummaryEntry(
	ctx context.Context,
	sessionID string,
	parentID *string,
	entryType EntryType,
	summary string,
	dataJSON string,
) (*EntryEntity, error) {
	timestamp := repository.now().UTC()
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

	if err := repository.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
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
