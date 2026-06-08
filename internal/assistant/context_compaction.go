// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	defaultCompactionKeepRecentTokens = 20_000
	compactionSummaryPrompt           = `Summarize the conversation history below for a coding agent that will continue
this same session.

Preserve:
- the user's goals and current task
- important decisions and constraints
- files, commands, errors, and validation results mentioned
- pending next steps and open questions

Be concise but specific. Do not invent facts. Return only the summary.`
	compactionUpdatePrompt = `Update the existing compaction summary with the new conversation history below.

Rules:
- preserve important facts from the existing summary
- add new progress, decisions, files, commands, errors, validation results, next steps, and open questions
- remove details that are clearly obsolete
- be concise but specific
- do not invent facts
- return only the updated summary

Existing summary:
<summary>
%s
</summary>`
)

// CompactSession summarizes older model-facing context and appends a compaction entry.
func (runtime *Runtime) CompactSession(
	ctx context.Context,
	sessionID string,
	cwd string,
) (*database.EntryEntity, error) {
	return runtime.CompactSessionFrom(ctx, sessionID, cwd, nil)
}

// CompactSessionFrom compacts the branch ending at parentEntryID, or the latest leaf when nil.
func (runtime *Runtime) CompactSessionFrom(
	ctx context.Context,
	sessionID string,
	cwd string,
	parentEntryID *string,
) (*database.EntryEntity, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, oops.In("assistant").Code("compact_no_session").Errorf("no active session to compact")
	}
	if runtime.models == nil {
		return nil, oops.In("assistant").Code("models_unavailable").Errorf("model registry is not configured")
	}

	selectedModel, auth, err := runtime.compactionModelAuth(ctx)
	if err != nil {
		return nil, err
	}
	parentID, branch, err := runtime.compactionBranch(ctx, sessionID, parentEntryID)
	if err != nil {
		return nil, err
	}
	plan, err := planCompaction(branch, runtime.cfg.Context.KeepRecentTokens)
	if err != nil {
		return nil, err
	}
	plan.FileOperations = collectCompactionFileOperations(branch[:plan.FirstKeptEntryIndex])

	return runtime.compactSessionWithPlan(ctx, sessionID, cwd, parentID, selectedModel, auth, &plan)
}

func (runtime *Runtime) compactSessionWithPlan(
	ctx context.Context,
	sessionID string,
	cwd string,
	parentID *string,
	selectedModel *model.Model,
	auth model.RequestAuth,
	plan *compactionPlan,
) (*database.EntryEntity, error) {
	summary, decision, fromHook, err := runtime.compactionSummaryDecision(
		ctx,
		cwd,
		sessionID,
		selectedModel,
		auth,
		plan,
	)
	if err != nil {
		return nil, err
	}
	if decision != nil && decision.FirstKeptEntryID != "" {
		plan.FirstKeptEntryID = decision.FirstKeptEntryID
	}

	entry, err := runtime.appendCompaction(ctx, sessionID, parentID, summary, plan, decision, fromHook)
	if err != nil {
		return nil, err
	}
	runtime.dispatchAfterCompaction(ctx, sessionID, cwd, entry, plan, fromHook)
	return entry, nil
}

func (runtime *Runtime) compactionModelAuth(ctx context.Context) (*model.Model, model.RequestAuth, error) {
	selectedModel, err := runtime.selectedModel()
	if err != nil {
		return nil, model.RequestAuth{}, err
	}
	auth := runtime.models.RequestAuthContext(ctx, selectedModel.Provider)
	if !auth.OK {
		return nil, model.RequestAuth{}, oops.In("assistant").
			Code("auth_missing").
			With("provider", selectedModel.Provider).
			Wrapf(fmt.Errorf("%s", auth.Error), "resolve model auth")
	}

	return &selectedModel, auth, nil
}

func (runtime *Runtime) compactionBranch(
	ctx context.Context,
	sessionID string,
	parentEntryID *string,
) (*string, []database.EntryEntity, error) {
	if parentEntryID != nil {
		return runtime.explicitCompactionBranch(ctx, sessionID, parentEntryID)
	}

	leaf, _, err := runtime.sessions.LeafEntry(ctx, sessionID)
	if err != nil {
		return nil, nil, oops.In("assistant").Code("compact_leaf").Wrapf(err, "load session leaf")
	}
	leafID := ""
	var parentID *string
	if leaf != nil {
		leafID = leaf.ID
		parentID = &leaf.ID
	}
	branch, err := runtime.sessions.Branch(ctx, sessionID, leafID)
	if err != nil {
		return nil, nil, oops.In("assistant").Code("compact_branch").Wrapf(err, "load session branch")
	}

	return parentID, branch, nil
}

func (runtime *Runtime) explicitCompactionBranch(
	ctx context.Context,
	sessionID string,
	parentEntryID *string,
) (*string, []database.EntryEntity, error) {
	if strings.TrimSpace(*parentEntryID) == "" {
		return nil, []database.EntryEntity{}, nil
	}
	branch, err := runtime.sessions.Branch(ctx, sessionID, *parentEntryID)
	if err != nil {
		return nil, nil, oops.In("assistant").Code("compact_branch").Wrapf(err, "load session branch")
	}

	return parentEntryID, branch, nil
}

func (runtime *Runtime) compactionSummaryDecision(
	ctx context.Context,
	cwd string,
	sessionID string,
	selectedModel *model.Model,
	auth model.RequestAuth,
	plan *compactionPlan,
) (summary string, decision *compactionLifecycleDecision, fromHook bool, err error) {
	decision, err = runtime.dispatchBeforeCompaction(ctx, sessionID, cwd, plan)
	if errors.Is(err, errNoCompactionDecision) {
		decision = nil
	} else if err != nil {
		return "", nil, false, err
	}
	if decision != nil && decision.Summary != "" {
		return appendFileOperationsSummary(decision.Summary, plan.FileOperations), decision, true, nil
	}
	summary, err = runtime.summarizeCompaction(ctx, cwd, sessionID, selectedModel, auth, plan)
	if err != nil {
		return "", nil, false, err
	}

	return summary, decision, false, nil
}

func (runtime *Runtime) appendCompaction(
	ctx context.Context,
	sessionID string,
	parentID *string,
	summary string,
	plan *compactionPlan,
	decision *compactionLifecycleDecision,
	fromHook bool,
) (*database.EntryEntity, error) {
	details := map[string]any{
		"summarized_entries":      len(plan.SummarizedEntryIDs),
		"kept_entries":            len(plan.KeptEntryIDs),
		compactionTokensBeforeKey: plan.TokensBefore,
	}
	if decision != nil {
		for key, value := range decision.Details {
			details[key] = value
		}
	}
	if len(plan.FileOperations) > 0 {
		details[compactionFileOperationsKey] = plan.FileOperations
	}
	entry, err := runtime.sessions.AppendCompaction(
		ctx,
		sessionID,
		parentID,
		summary,
		plan.FirstKeptEntryID,
		plan.TokensBefore,
		details,
		fromHook,
	)
	if err != nil {
		return nil, oops.In("assistant").Code("append_compaction").Wrapf(err, "append compaction")
	}
	runtime.dispatchMessageAppend(ctx, entry)

	return entry, nil
}

type compactionPlan struct {
	FirstKeptEntryID    string
	Messages            []database.MessageEntity
	PreviousSummary     string
	SplitTurnSummary    string
	SummarizedEntryIDs  []string
	KeptEntryIDs        []string
	FileOperations      []compactionFileOperation
	TokensBefore        int
	FirstKeptEntryIndex int
}

func planCompaction(branch []database.EntryEntity, keepRecentTokens int) (compactionPlan, error) {
	if len(branch) == 0 {
		return compactionPlan{}, compactNothingToDoError("not enough model-facing history to compact")
	}
	if branch[len(branch)-1].Type == database.EntryTypeCompaction {
		return compactionPlan{}, compactNothingToDoError("no new history to compact after the latest compaction")
	}
	if keepRecentTokens <= 0 {
		keepRecentTokens = defaultCompactionKeepRecentTokens
	}

	previousSummary, boundaryStart := previousCompactionBoundary(branch)
	cutPoint := findCompactionCutPoint(branch, boundaryStart, len(branch), keepRecentTokens)
	if cutPoint.firstKeptEntryIndex <= boundaryStart || cutPoint.firstKeptEntryIndex >= len(branch) {
		return compactionPlan{}, compactNothingToDoError(
			"not enough old history to compact while preserving the recent tail",
		)
	}

	messages, summarizedIDs, splitTurnSummary := compactionSummaryPayload(branch, boundaryStart, cutPoint)
	if len(messages) == 0 && strings.TrimSpace(splitTurnSummary) == "" {
		return compactionPlan{}, compactNothingToDoError("no model-facing history was selected for compaction")
	}

	keptIDs := keptCompactionEntryIDs(branch[cutPoint.firstKeptEntryIndex:])
	firstKeptEntryID := branch[cutPoint.firstKeptEntryIndex].ID

	return compactionPlan{
		Messages:            messages,
		PreviousSummary:     previousSummary,
		SplitTurnSummary:    splitTurnSummary,
		SummarizedEntryIDs:  summarizedIDs,
		KeptEntryIDs:        keptIDs,
		FileOperations:      nil,
		FirstKeptEntryID:    firstKeptEntryID,
		TokensBefore:        effectiveBranchTokens(branch),
		FirstKeptEntryIndex: cutPoint.firstKeptEntryIndex,
	}, nil
}

func compactionSummaryPayload(
	branch []database.EntryEntity,
	boundaryStart int,
	cutPoint compactionCutPoint,
) (messages []database.MessageEntity, summarizedIDs []string, splitTurnSummary string) {
	summarizeEnd := cutPoint.firstKeptEntryIndex
	if cutPoint.isSplitTurn {
		summarizeEnd = cutPoint.turnStartIndex
	}
	messages, summarizedIDs = compactionMessagesInRange(branch, boundaryStart, summarizeEnd)
	if !cutPoint.isSplitTurn {
		return messages, summarizedIDs, ""
	}

	turnPrefixMessages, turnPrefixIDs := compactionMessagesInRange(
		branch,
		cutPoint.turnStartIndex,
		cutPoint.firstKeptEntryIndex,
	)
	summarizedIDs = append(summarizedIDs, turnPrefixIDs...)
	if len(messages) == 0 {
		return append(messages, turnPrefixMessages...), summarizedIDs, ""
	}

	return messages, summarizedIDs, formatSplitTurnSummary(turnPrefixMessages)
}

func compactNothingToDoError(message string) error {
	return oops.In("assistant").Code("compact_nothing_to_do").Errorf("%s", message)
}

func previousCompactionBoundary(branch []database.EntryEntity) (summary string, boundaryStart int) {
	for index := len(branch) - 1; index >= 0; index-- {
		entry := &branch[index]
		if entry.Type != database.EntryTypeCompaction {
			continue
		}
		firstKeptIndex := entryIndexByID(branch, entry.CompactionFirstKeptEntryID)
		if firstKeptIndex < 0 {
			firstKeptIndex = index + 1
		}

		return entry.Summary, firstKeptIndex
	}

	return "", 0
}

func entryIndexByID(entries []database.EntryEntity, entryID string) int {
	for index := range entries {
		if entries[index].ID == entryID {
			return index
		}
	}

	return -1
}

type compactionCutPoint struct {
	firstKeptEntryIndex int
	turnStartIndex      int
	isSplitTurn         bool
}

func findCompactionCutPoint(
	entries []database.EntryEntity,
	startIndex int,
	endIndex int,
	keepRecentTokens int,
) compactionCutPoint {
	cutPoints := validCompactionCutPoints(entries, startIndex, endIndex)
	if len(cutPoints) == 0 {
		return compactionCutPoint{firstKeptEntryIndex: startIndex, turnStartIndex: -1, isSplitTurn: false}
	}

	accumulatedTokens := 0
	cutIndex := cutPoints[0]
	for index := endIndex - 1; index >= startIndex; index-- {
		message, ok := messageForCompactionSummary(&entries[index])
		if !ok {
			continue
		}
		accumulatedTokens += estimateTokens(message.Content)
		if accumulatedTokens < keepRecentTokens {
			continue
		}
		cutIndex = firstCutPointAtOrAfter(cutPoints, index)
		break
	}

	turnStartIndex := -1
	if !isTurnStartEntry(&entries[cutIndex]) {
		turnStartIndex = findTurnStartEntryIndex(entries, cutIndex, startIndex)
	}

	return compactionCutPoint{
		firstKeptEntryIndex: cutIndex,
		turnStartIndex:      turnStartIndex,
		isSplitTurn:         turnStartIndex >= 0,
	}
}

func validCompactionCutPoints(entries []database.EntryEntity, startIndex, endIndex int) []int {
	cutPoints := []int{}
	for index := startIndex; index < endIndex; index++ {
		if isValidCompactionCutPoint(&entries[index]) {
			cutPoints = append(cutPoints, index)
		}
	}

	return cutPoints
}

func isValidCompactionCutPoint(entry *database.EntryEntity) bool {
	_, ok := messageForCompactionSummary(entry)

	return ok
}

func firstCutPointAtOrAfter(cutPoints []int, entryIndex int) int {
	for index := range cutPoints {
		if cutPoints[index] >= entryIndex {
			return cutPoints[index]
		}
	}

	return cutPoints[len(cutPoints)-1]
}

func findTurnStartEntryIndex(entries []database.EntryEntity, entryIndex, startIndex int) int {
	for index := entryIndex; index >= startIndex; index-- {
		if isTurnStartEntry(&entries[index]) {
			return index
		}
	}

	return -1
}

func isTurnStartEntry(entry *database.EntryEntity) bool {
	message, ok := messageForCompactionSummary(entry)
	if !ok {
		return false
	}

	return message.Role == database.RoleUser || message.Role == database.RoleCustom
}

func compactionMessagesInRange(
	entries []database.EntryEntity,
	startIndex int,
	endIndex int,
) (messages []database.MessageEntity, entryIDs []string) {
	messages = make([]database.MessageEntity, 0, endIndex-startIndex)
	entryIDs = make([]string, 0, endIndex-startIndex)
	for index := startIndex; index < endIndex; index++ {
		message, ok := messageForCompactionSummary(&entries[index])
		if !ok {
			continue
		}
		messages = append(messages, modelFacingMessage(&message))
		entryIDs = append(entryIDs, entries[index].ID)
	}

	return messages, entryIDs
}

func formatSplitTurnSummary(messages []database.MessageEntity) string {
	if len(messages) == 0 {
		return ""
	}
	builder := strings.Builder{}
	builder.WriteString("The compaction boundary split an in-progress turn. ")
	builder.WriteString("The following earlier messages from that turn were compacted:\n")
	for index := range messages {
		message := messages[index]
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		builder.WriteString("\n[")
		builder.WriteString(string(message.Role))
		builder.WriteString("]\n")
		builder.WriteString(content)
		builder.WriteString("\n")
	}

	return strings.TrimSpace(builder.String())
}

func keptCompactionEntryIDs(entries []database.EntryEntity) []string {
	entryIDs := make([]string, 0, len(entries))
	for index := range entries {
		if _, ok := messageForCompactionContext(&entries[index]); ok {
			entryIDs = append(entryIDs, entries[index].ID)
		}
	}

	return entryIDs
}

func effectiveBranchTokens(branch []database.EntryEntity) int {
	effective := effectiveModelFacingBranchEntries(branch)
	messages := make([]database.MessageEntity, 0, len(effective))
	for index := range effective {
		message, ok := messageForCompactionContext(&effective[index])
		if !ok {
			continue
		}
		messages = append(messages, modelFacingMessage(&message))
	}

	return estimateMessageTokens(messages)
}

func effectiveModelFacingBranchEntries(branch []database.EntryEntity) []database.EntryEntity {
	entries := modelFacingBranchEntries(branch)
	effective := make([]database.EntryEntity, 0, len(entries))
	for index := range entries {
		entry := entries[index]
		if entry.Type != database.EntryTypeCompaction {
			effective = append(effective, entry)
			continue
		}

		firstKeptIndex := len(effective)
		for effectiveIndex := range effective {
			if effective[effectiveIndex].ID == entry.CompactionFirstKeptEntryID {
				firstKeptIndex = effectiveIndex
				break
			}
		}
		compacted := make([]database.EntryEntity, 0, 1+len(effective)-firstKeptIndex)
		compacted = append(compacted, entry)
		compacted = append(compacted, effective[firstKeptIndex:]...)
		effective = compacted
	}

	return effective
}

func modelFacingBranchEntries(branch []database.EntryEntity) []database.EntryEntity {
	entries := make([]database.EntryEntity, 0, len(branch))
	for index := range branch {
		entry := branch[index]
		message, ok := messageForCompactionContext(&entry)
		if !ok {
			continue
		}
		entry.Message = message
		entries = append(entries, entry)
	}

	return entries
}

func emptyMessageEntity() database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{},
		Role:      "",
		Content:   "",
		Provider:  "",
		Model:     "",
	}
}

func messageForCompactionContext(entry *database.EntryEntity) (database.MessageEntity, bool) {
	message := compactionCandidateMessage(entry)
	if !entry.ModelFacing || !isModelFacingRole(message.Role) || strings.TrimSpace(message.Content) == "" {
		return emptyMessageEntity(), false
	}

	return message, true
}

func messageForCompactionSummary(entry *database.EntryEntity) (database.MessageEntity, bool) {
	if entry.Type == database.EntryTypeCompaction {
		return emptyMessageEntity(), false
	}

	return messageForCompactionContext(entry)
}

func compactionCandidateMessage(entry *database.EntryEntity) database.MessageEntity {
	message := entry.Message
	switch entry.Type {
	case database.EntryTypeBranchSummary:
		message.Role = database.RoleBranchSummary
		message.Content = entry.Summary
	case database.EntryTypeCompaction:
		message.Role = database.RoleCompactionSummary
		message.Content = entry.Summary
	case database.EntryTypeMessage,
		database.EntryTypeCustomMessage,
		database.EntryTypeCustom,
		database.EntryTypeLabel,
		database.EntryTypeModelChange,
		database.EntryTypeSessionInfo,
		database.EntryTypeThinkingLevelChange:
	}

	return message
}

func (runtime *Runtime) summarizeCompaction(
	ctx context.Context,
	cwd string,
	sessionID string,
	selectedModel *model.Model,
	auth model.RequestAuth,
	plan *compactionPlan,
) (string, error) {
	systemPrompt := compactionSystemPromptWithSplit(plan.PreviousSummary, plan.SplitTurnSummary)
	request := &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: runtime.emitProviderRequest,
		OnProviderRequest: runtime.dispatchProviderRequestHook,
		OnToolCall:        nil,
		OnToolResult:      nil,
		ToolRegistry:      tool.NewRegistry(cwd),
		ExecuteTools:      nil,
		DisableTools:      true,
		SessionID:         sessionID,
		SystemPrompt:      systemPrompt,
		ThinkingLevel:     thinkingOff,
		CWD:               cwd,
		Auth:              auth,
		Messages:          plan.Messages,
		Usage:             compactionRequestUsage(selectedModel, systemPrompt, plan.Messages),
		Model:             *selectedModel,
		ProviderAttempt:   0,
	}
	result, err := runtime.completeWithRetry(ctx, request, nil)
	if err != nil {
		return "", oops.In("assistant").Code("compact_summarize").Wrapf(err, "summarize compacted context")
	}
	summary := strings.TrimSpace(result.Text)
	if summary == "" {
		return "", oops.In("assistant").Code("compact_empty_summary").Errorf("compaction summary was empty")
	}

	return appendFileOperationsSummary(summary, plan.FileOperations), nil
}

func compactionSystemPrompt(previousSummary string) string {
	return compactionSystemPromptWithSplit(previousSummary, "")
}

func compactionSystemPromptWithSplit(previousSummary, splitTurnSummary string) string {
	prompt := compactionBaseSystemPrompt(previousSummary)
	if strings.TrimSpace(splitTurnSummary) == "" {
		return prompt
	}

	return prompt + "\n\n" + splitTurnPromptSection(splitTurnSummary)
}

func compactionBaseSystemPrompt(previousSummary string) string {
	if strings.TrimSpace(previousSummary) == "" {
		return compactionSummaryPrompt
	}

	return fmt.Sprintf(compactionUpdatePrompt, previousSummary)
}

func splitTurnPromptSection(splitTurnSummary string) string {
	return strings.TrimSpace(`Additional split-turn context:
<split_turn_summary>
` + strings.TrimSpace(splitTurnSummary) + `
</split_turn_summary>`)
}

func compactionRequestUsage(
	selectedModel *model.Model,
	systemPrompt string,
	messages []database.MessageEntity,
) model.TokenUsage {
	contextWindow := 0
	if selectedModel != nil {
		contextWindow = selectedModel.ContextWindow
	}
	inputTokens := estimateInputTokens(systemPrompt, messages)

	return model.TokenUsage{
		Breakdown: map[string]int{
			jsonSystemRole:          estimateTokens(systemPrompt),
			contextBreakdownHistory: estimateMessageTokens(messages),
		},
		TopContributors: nil,
		ContextWindow:   contextWindow,
		ContextTokens:   inputTokens,
		InputTokens:     inputTokens,
		OutputTokens:    0,
	}
}
