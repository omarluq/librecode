package database

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/samber/oops"
)

const appendCompactionNilInputCode = "append_compaction_nil_input"

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
		UsageAnchor:   nil,
		Messages:      []MessageEntity{},
		Provider:      "",
		Model:         "",
		ThinkingLevel: "",
	}

	for index := range branch {
		if branch[index].Type == EntryTypeCompaction {
			if err := applyCompactionContextWithTail(&contextEntity, branch, index); err != nil {
				return nil, err
			}

			continue
		}

		if err := applyEntryToContext(&contextEntity, &branch[index]); err != nil {
			return nil, err
		}
	}

	return &contextEntity, nil
}

func newEntryEntity(sessionID string, parentID *string, entryType EntryType, message *MessageEntity) EntryEntity {
	createdAt := message.Timestamp

	return EntryEntity{
		CreatedAt:                  createdAt,
		ParentID:                   parentID,
		Message:                    *message,
		Summary:                    "",
		ToolStatus:                 "",
		Type:                       entryType,
		CustomType:                 "",
		DataJSON:                   "{}",
		ID:                         newEntryID(),
		ToolName:                   "",
		SessionID:                  sessionID,
		ToolArgsJSON:               "",
		BranchFromEntryID:          "",
		CompactionFirstKeptEntryID: "",
		CompactionTokensBefore:     0,
		TokenEstimate:              0,
		Display:                    true,
		ModelFacing:                false,
	}
}

func newEntryData() EntryDataEntity {
	var data EntryDataEntity

	return data
}

type appendEntryOptions struct {
	modelFacing *bool
	timestamp   time.Time
	usage       *EntryTokenUsageEntity
	content     string
	customType  string
	dataJSON    string
	model       string
	provider    string
	summary     string
	role        Role
}

func newAppendEntryOptions() *appendEntryOptions {
	var options appendEntryOptions

	return &options
}

func (repository *SessionRepository) appendBuiltEntry(
	ctx context.Context,
	sessionID string,
	parentID *string,
	entryType EntryType,
	options *appendEntryOptions,
) (*EntryEntity, error) {
	entry := repository.entryFromAppendOptions(sessionID, parentID, entryType, options)
	if err := applyAppendEntryMetadata(&entry, options); err != nil {
		return nil, err
	}

	if err := repository.appendEntry(ctx, &entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (repository *SessionRepository) entryFromAppendOptions(
	sessionID string,
	parentID *string,
	entryType EntryType,
	options *appendEntryOptions,
) EntryEntity {
	timestamp := options.timestamp
	if timestamp.IsZero() {
		timestamp = repository.now().UTC()
	} else {
		timestamp = timestamp.UTC()
	}

	entry := newEntryEntity(sessionID, parentID, entryType, &MessageEntity{
		Timestamp: timestamp,
		Role:      options.role,
		Content:   options.content,
		Provider:  options.provider,
		Model:     options.model,
	})

	entry.CustomType = options.customType
	if options.dataJSON != "" {
		entry.DataJSON = normalizeDataJSON(options.dataJSON)
	}

	entry.Summary = options.summary

	return entry
}

func applyAppendEntryMetadata(entry *EntryEntity, options *appendEntryOptions) error {
	if options.modelFacing == nil && options.usage == nil {
		return nil
	}

	data, err := dataFromEntry(entry)
	if err != nil {
		return oops.In("database").
			Code("decode_entry_data").
			Wrapf(err, "decode entry data before setting metadata")
	}

	if options.modelFacing != nil {
		data.ModelFacing = options.modelFacing
	}

	if options.usage != nil {
		data.Usage = options.usage
	}

	dataJSON, err := dataJSONFromEntity(&data)
	if err != nil {
		return oops.In("database").
			Code("encode_entry_data").
			Wrapf(err, "encode entry data after setting metadata")
	}

	entry.DataJSON = normalizeDataJSON(dataJSON)

	return nil
}

// AppendMessage appends a message as a child of the current leaf or provided parent.
func (repository *SessionRepository) AppendMessage(
	ctx context.Context,
	sessionID string,
	parentID *string,
	message *MessageEntity,
) (*EntryEntity, error) {
	return repository.AppendMessageWithModelFacing(ctx, sessionID, parentID, message, nil)
}

// AppendMessageWithModelFacing appends a message with an optional model-facing override.
func (repository *SessionRepository) AppendMessageWithModelFacing(
	ctx context.Context,
	sessionID string,
	parentID *string,
	message *MessageEntity,
	modelFacing *bool,
) (*EntryEntity, error) {
	return repository.AppendMessageWithMetadata(ctx, sessionID, parentID, message, modelFacing, nil)
}

// AppendMessageWithMetadata appends a message with optional model-facing and token usage metadata.
func (repository *SessionRepository) AppendMessageWithMetadata(
	ctx context.Context,
	sessionID string,
	parentID *string,
	message *MessageEntity,
	modelFacing *bool,
	usage *EntryTokenUsageEntity,
) (*EntryEntity, error) {
	options := newAppendEntryOptions()
	options.content = message.Content
	options.model = message.Model
	options.provider = message.Provider
	options.role = message.Role
	options.timestamp = message.Timestamp
	options.modelFacing = modelFacing
	options.usage = usage

	return repository.appendBuiltEntry(ctx, sessionID, parentID, EntryTypeMessage, options)
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
	options := newAppendEntryOptions()
	options.customType = customType
	options.dataJSON = dataJSON

	return repository.appendBuiltEntry(ctx, sessionID, parentID, EntryTypeCustom, options)
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
	data := newEntryData()
	data.Details = details
	data.Display = &display
	data.ModelFacing = &display

	dataJSON, err := dataJSONFromEntity(&data)
	if err != nil {
		return nil, err
	}

	options := newAppendEntryOptions()
	options.content = content
	options.customType = customType
	options.dataJSON = dataJSON
	options.role = RoleCustom
	options.modelFacing = &display

	return repository.appendBuiltEntry(ctx, sessionID, parentID, EntryTypeCustomMessage, options)
}

// AppendModelChange records a provider/model switch.
func (repository *SessionRepository) AppendModelChange(
	ctx context.Context,
	sessionID string,
	parentID *string,
	provider string,
	model string,
) (*EntryEntity, error) {
	options := newAppendEntryOptions()
	options.model = model
	options.provider = provider

	return repository.appendBuiltEntry(ctx, sessionID, parentID, EntryTypeModelChange, options)
}

// AppendThinkingLevelChange records a reasoning/thinking level switch.
func (repository *SessionRepository) AppendThinkingLevelChange(
	ctx context.Context,
	sessionID string,
	parentID *string,
	thinkingLevel string,
) (*EntryEntity, error) {
	data := newEntryData()
	data.ThinkingLevel = thinkingLevel

	dataJSON, err := dataJSONFromEntity(&data)
	if err != nil {
		return nil, err
	}

	options := newAppendEntryOptions()
	options.dataJSON = dataJSON

	return repository.appendBuiltEntry(ctx, sessionID, parentID, EntryTypeThinkingLevelChange, options)
}

// AppendCompactionInput describes a compaction summary entry to append.
type AppendCompactionInput struct {
	ParentID         *string
	Details          map[string]any
	SessionID        string
	Summary          string
	FirstKeptEntryID string
	TokensBefore     int
	FromHook         bool
}

// AppendCompaction records a summary for compacted context.
func (repository *SessionRepository) AppendCompaction(
	ctx context.Context,
	input *AppendCompactionInput,
) (*EntryEntity, error) {
	if input == nil {
		return nil, oops.In("database").Code(appendCompactionNilInputCode).Errorf("append compaction input is nil")
	}

	data := newEntryData()
	data.Details = input.Details
	data.FromHook = input.FromHook
	data.FirstKeptEntryID = input.FirstKeptEntryID
	data.CompactionFirstKeptEntryID = input.FirstKeptEntryID
	data.CompactionTokensBefore = input.TokensBefore
	data.TokensBefore = input.TokensBefore

	dataJSON, err := dataJSONFromEntity(&data)
	if err != nil {
		return nil, err
	}

	return repository.appendSummaryEntry(
		ctx,
		input.SessionID,
		input.ParentID,
		EntryTypeCompaction,
		input.Summary,
		dataJSON,
	)
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
	data := newEntryData()
	data.Details = details
	data.FromHook = fromHook
	data.FromID = fromID
	data.BranchFromEntryID = fromID

	dataJSON, err := dataJSONFromEntity(&data)
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
	data := newEntryData()
	data.Label = label
	data.TargetID = targetID

	dataJSON, err := dataJSONFromEntity(&data)
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
	data := newEntryData()
	data.Name = name

	dataJSON, err := dataJSONFromEntity(&data)
	if err != nil {
		return nil, err
	}

	entry, err := repository.appendSummaryEntry(ctx, sessionID, parentID, EntryTypeSessionInfo, "", dataJSON)
	if err != nil {
		return nil, err
	}

	const statement = `UPDATE sessions SET name = ? WHERE id = ?`
	if _, err := repository.sql.Exec(ctx, statement, name, sessionID); err != nil {
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
	options := newAppendEntryOptions()
	options.dataJSON = dataJSON
	options.summary = summary

	return repository.appendBuiltEntry(ctx, sessionID, parentID, entryType, options)
}

func dataJSONFromEntity(data *EntryDataEntity) (string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("database: encode entry data: %w", err)
	}

	return string(encoded), nil
}

func dataFromEntry(entry *EntryEntity) (EntryDataEntity, error) {
	data := newEntryData()
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
	case EntryTypeMessage, EntryTypeCustomMessage:
		return appendModelFacingEntryMessage(contextEntity, entry)
	case EntryTypeBranchSummary:
		return appendBranchSummaryContext(contextEntity, entry)
	case EntryTypeModelChange:
		return applyModelChangeContext(contextEntity, entry)
	case EntryTypeThinkingLevelChange:
		return applyThinkingLevelContext(contextEntity, entry)
	case EntryTypeCustom, EntryTypeCompaction, EntryTypeLabel, EntryTypeSessionInfo:
		return nil
	}

	return nil
}

func appendModelFacingEntryMessage(contextEntity *SessionContextEntity, entry *EntryEntity) error {
	if entry.ModelFacing {
		contextEntity.Messages = append(contextEntity.Messages, entry.Message)
		if err := updateContextUsageAnchor(contextEntity, entry, len(contextEntity.Messages)-1); err != nil {
			return err
		}
	}

	return nil
}

func updateContextUsageAnchor(contextEntity *SessionContextEntity, entry *EntryEntity, messageIndex int) error {
	if entry.Type != EntryTypeMessage || entry.Message.Role != RoleAssistant {
		return nil
	}

	data, err := dataFromEntry(entry)
	if err != nil {
		return oops.In("database").
			Code("decode_entry_usage").
			Wrapf(err, "decode assistant entry usage")
	}

	if data.Usage == nil || !data.Usage.HasAny() {
		return nil
	}

	contextEntity.UsageAnchor = &ContextUsageAnchorEntity{
		EntryID:      entry.ID,
		MessageIndex: messageIndex,
		Provider:     entry.Message.Provider,
		Model:        entry.Message.Model,
		Usage:        *data.Usage,
	}

	return nil
}

func appendBranchSummaryContext(contextEntity *SessionContextEntity, entry *EntryEntity) error {
	contextEntity.Messages = append(contextEntity.Messages, MessageEntity{
		Timestamp: entry.CreatedAt,
		Role:      RoleBranchSummary,
		Content:   entry.Summary,
		Provider:  "",
		Model:     "",
	})

	return nil
}

func applyCompactionContextWithTail(
	contextEntity *SessionContextEntity,
	branch []EntryEntity,
	compactionIndex int,
) error {
	compactionEntry := &branch[compactionIndex]
	contextEntity.Messages = compactionSummaryMessages(compactionEntry)
	clearCompactedUsageAnchor(contextEntity)

	firstKeptIndex := compactionIndex
	for index := range compactionIndex {
		if branch[index].ID == compactionEntry.CompactionFirstKeptEntryID {
			firstKeptIndex = index

			break
		}
	}

	for index := firstKeptIndex; index < compactionIndex; index++ {
		if err := appendRetainedCompactionTailEntry(contextEntity, &branch[index]); err != nil {
			return err
		}
	}

	return nil
}

func clearCompactedUsageAnchor(contextEntity *SessionContextEntity) {
	// Provider usage anchors describe the pre-compaction prompt shape. After a
	// compaction entry rewrites context to summary + tail, that usage is stale.
	contextEntity.UsageAnchor = nil
}

func compactionSummaryMessages(entry *EntryEntity) []MessageEntity {
	return []MessageEntity{{
		Timestamp: entry.CreatedAt,
		Role:      RoleCompactionSummary,
		Content:   entry.Summary,
		Provider:  "",
		Model:     "",
	}}
}

func appendRetainedCompactionTailEntry(contextEntity *SessionContextEntity, entry *EntryEntity) error {
	switch entry.Type {
	case EntryTypeMessage, EntryTypeCustomMessage:
		if entry.ModelFacing {
			contextEntity.Messages = append(contextEntity.Messages, entry.Message)
		}

		return nil
	case EntryTypeBranchSummary:
		return appendBranchSummaryContext(contextEntity, entry)
	case EntryTypeCompaction,
		EntryTypeModelChange,
		EntryTypeThinkingLevelChange,
		EntryTypeCustom,
		EntryTypeLabel,
		EntryTypeSessionInfo:
		return nil
	default:
		return nil
	}
}

func applyModelChangeContext(contextEntity *SessionContextEntity, entry *EntryEntity) error {
	contextEntity.Provider = entry.Message.Provider
	contextEntity.Model = entry.Message.Model

	return nil
}

func applyThinkingLevelContext(contextEntity *SessionContextEntity, entry *EntryEntity) error {
	data, err := dataFromEntry(entry)
	if err != nil {
		return oops.In("database").
			Code("decode_entry_data").
			Wrapf(err, "decode thinking level entry data")
	}

	contextEntity.ThinkingLevel = data.ThinkingLevel

	return nil
}
