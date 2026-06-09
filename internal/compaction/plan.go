package compaction

import (
	"fmt"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
)

const (
	defaultKeepRecentTokens = 20_000
	summaryPrompt           = `Summarize the conversation history below for a coding agent that will continue
this same session.

Preserve:
- the user's goals and current task
- important decisions and constraints
- files, commands, errors, and validation results mentioned
- pending next steps and open questions

Be concise but specific. Do not invent facts. Return only the summary.`
	updatePrompt = `Update the existing compaction summary with the new conversation history below.

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

// TokenCounter estimates token usage for text.
type TokenCounter func(string) int

// Plan describes the history range selected for compaction and the retained tail.
type Plan struct {
	FirstKeptEntryID    string
	Messages            []database.MessageEntity
	PreviousSummary     string
	SplitTurnSummary    string
	SummarizedEntryIDs  []string
	KeptEntryIDs        []string
	FileOperations      []FileOperation
	TokensBefore        int
	FirstKeptEntryIndex int
}

// PlanBranch selects model-facing branch history for compaction while preserving a recent tail.
func PlanBranch(
	branch []database.EntryEntity,
	keepRecentTokens int,
	countTokens TokenCounter,
) (Plan, error) {
	if len(branch) == 0 {
		return Plan{}, NothingToDoError("not enough model-facing history to compact")
	}
	if branch[len(branch)-1].Type == database.EntryTypeCompaction {
		return Plan{}, NothingToDoError("no new history to compact after the latest compaction")
	}
	if keepRecentTokens <= 0 {
		keepRecentTokens = defaultKeepRecentTokens
	}
	if countTokens == nil {
		countTokens = countRunesAsTokens
	}

	previousSummary, boundaryStart := previousCompactionBoundary(branch)
	cutPoint := findCutPoint(branch, boundaryStart, len(branch), keepRecentTokens, countTokens)
	if cutPoint.firstKeptEntryIndex <= boundaryStart || cutPoint.firstKeptEntryIndex >= len(branch) {
		return Plan{}, NothingToDoError("not enough old history to compact while preserving the recent tail")
	}

	messages, summarizedIDs, splitTurnSummary := summaryPayload(branch, boundaryStart, cutPoint)
	if len(messages) == 0 && strings.TrimSpace(splitTurnSummary) == "" {
		return Plan{}, NothingToDoError("no model-facing history was selected for compaction")
	}

	firstKeptEntryID := branch[cutPoint.firstKeptEntryIndex].ID

	return Plan{
		Messages:            messages,
		PreviousSummary:     previousSummary,
		SplitTurnSummary:    splitTurnSummary,
		SummarizedEntryIDs:  summarizedIDs,
		KeptEntryIDs:        keptEntryIDs(branch[cutPoint.firstKeptEntryIndex:]),
		FileOperations:      nil,
		FirstKeptEntryID:    firstKeptEntryID,
		TokensBefore:        branchTokens(branch, countTokens),
		FirstKeptEntryIndex: cutPoint.firstKeptEntryIndex,
	}, nil
}

// NothingToDoError returns the no-op compaction error used by planner callers.
func NothingToDoError(message string) error {
	return oops.In("compaction").Code("compact_nothing_to_do").Errorf("%s", message)
}

func summaryPayload(
	branch []database.EntryEntity,
	boundaryStart int,
	cutPoint cutPoint,
) (messages []database.MessageEntity, summarizedIDs []string, splitTurnSummary string) {
	summarizeEnd := cutPoint.firstKeptEntryIndex
	if cutPoint.isSplitTurn {
		summarizeEnd = cutPoint.turnStartIndex
	}
	messages, summarizedIDs = messagesInRange(branch, boundaryStart, summarizeEnd)
	if !cutPoint.isSplitTurn {
		return messages, summarizedIDs, ""
	}

	turnPrefixMessages, turnPrefixIDs := messagesInRange(
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

type cutPoint struct {
	firstKeptEntryIndex int
	turnStartIndex      int
	isSplitTurn         bool
}

func findCutPoint(
	entries []database.EntryEntity,
	startIndex int,
	endIndex int,
	keepRecentTokens int,
	countTokens TokenCounter,
) cutPoint {
	cutPoints := validCutPoints(entries, startIndex, endIndex)
	if len(cutPoints) == 0 {
		return cutPoint{firstKeptEntryIndex: startIndex, turnStartIndex: -1, isSplitTurn: false}
	}

	accumulatedTokens := 0
	cutIndex := cutPoints[0]
	for index := endIndex - 1; index >= startIndex; index-- {
		message, ok := messageForSummary(&entries[index])
		if !ok {
			continue
		}
		accumulatedTokens += countTokens(message.Content)
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

	return cutPoint{
		firstKeptEntryIndex: cutIndex,
		turnStartIndex:      turnStartIndex,
		isSplitTurn:         turnStartIndex >= 0,
	}
}

func validCutPoints(entries []database.EntryEntity, startIndex, endIndex int) []int {
	cutPoints := []int{}
	for index := startIndex; index < endIndex; index++ {
		if isValidCutPoint(&entries[index]) {
			cutPoints = append(cutPoints, index)
		}
	}

	return cutPoints
}

func isValidCutPoint(entry *database.EntryEntity) bool {
	_, ok := messageForSummary(entry)

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
	message, ok := messageForSummary(entry)
	if !ok {
		return false
	}

	return message.Role == database.RoleUser || message.Role == database.RoleCustom
}

func messagesInRange(
	entries []database.EntryEntity,
	startIndex int,
	endIndex int,
) (messages []database.MessageEntity, entryIDs []string) {
	messages = make([]database.MessageEntity, 0, endIndex-startIndex)
	entryIDs = make([]string, 0, endIndex-startIndex)
	for index := startIndex; index < endIndex; index++ {
		message, ok := messageForSummary(&entries[index])
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

func keptEntryIDs(entries []database.EntryEntity) []string {
	entryIDs := make([]string, 0, len(entries))
	for index := range entries {
		if _, ok := messageForContext(&entries[index]); ok {
			entryIDs = append(entryIDs, entries[index].ID)
		}
	}

	return entryIDs
}

func branchTokens(branch []database.EntryEntity, countTokens TokenCounter) int {
	effective := effectiveModelFacingEntries(branch)
	messages := make([]database.MessageEntity, 0, len(effective))
	for index := range effective {
		message, ok := messageForContext(&effective[index])
		if !ok {
			continue
		}
		messages = append(messages, modelFacingMessage(&message))
	}

	return messageTokens(messages, countTokens)
}

func effectiveModelFacingEntries(branch []database.EntryEntity) []database.EntryEntity {
	entries := modelFacingEntries(branch)
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

func modelFacingEntries(branch []database.EntryEntity) []database.EntryEntity {
	entries := make([]database.EntryEntity, 0, len(branch))
	for index := range branch {
		entry := branch[index]
		message, ok := messageForContext(&entry)
		if !ok {
			continue
		}
		entry.Message = message
		entries = append(entries, entry)
	}

	return entries
}

func emptyMessage() database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Time{},
		Role:      "",
		Content:   "",
		Provider:  "",
		Model:     "",
	}
}

func messageForContext(entry *database.EntryEntity) (database.MessageEntity, bool) {
	message := candidateMessage(entry)
	if !entry.ModelFacing || !isModelFacingRole(message.Role) || strings.TrimSpace(message.Content) == "" {
		return emptyMessage(), false
	}

	return message, true
}

func messageForSummary(entry *database.EntryEntity) (database.MessageEntity, bool) {
	if entry.Type == database.EntryTypeCompaction {
		return emptyMessage(), false
	}

	return messageForContext(entry)
}

func candidateMessage(entry *database.EntryEntity) database.MessageEntity {
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

func modelFacingMessage(message *database.MessageEntity) database.MessageEntity {
	converted := *message
	switch message.Role {
	case database.RoleCompactionSummary:
		converted.Role = database.RoleUser
		converted.Content = core.CompactionSummaryPrefix + message.Content + core.CompactionSummarySuffix
	case database.RoleBranchSummary:
		converted.Role = database.RoleUser
		converted.Content = core.BranchSummaryPrefix + message.Content + core.BranchSummarySuffix
	case database.RoleUser,
		database.RoleAssistant,
		database.RoleToolResult,
		database.RoleThinking,
		database.RoleCustom,
		database.RoleBashExecution:
		return converted
	}

	return converted
}

func isModelFacingRole(role database.Role) bool {
	switch role {
	case database.RoleUser,
		database.RoleAssistant,
		database.RoleBranchSummary,
		database.RoleCompactionSummary,
		database.RoleCustom,
		database.RoleBashExecution:
		return true
	case database.RoleToolResult,
		database.RoleThinking:
		return false
	}

	return false
}

func messageTokens(messages []database.MessageEntity, countTokens TokenCounter) int {
	tokens := 0
	for index := range messages {
		tokens += countTokens(messages[index].Content)
	}

	return tokens
}

func countRunesAsTokens(text string) int {
	return len(strings.TrimSpace(text))
}

// SystemPrompt builds the summary prompt used for compaction provider requests.
func SystemPrompt(previousSummary, splitTurnSummary string) string {
	prompt := baseSystemPrompt(previousSummary)
	if strings.TrimSpace(splitTurnSummary) == "" {
		return prompt
	}

	return prompt + "\n\n" + splitTurnPromptSection(splitTurnSummary)
}

func baseSystemPrompt(previousSummary string) string {
	if strings.TrimSpace(previousSummary) == "" {
		return summaryPrompt
	}

	return fmt.Sprintf(updatePrompt, previousSummary)
}

func splitTurnPromptSection(splitTurnSummary string) string {
	return strings.TrimSpace(`Additional split-turn context:
<split_turn_summary>
` + strings.TrimSpace(splitTurnSummary) + `
</split_turn_summary>`)
}
