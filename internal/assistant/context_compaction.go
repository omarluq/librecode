// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"
	"strings"

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
)

// CompactSession summarizes older model-facing context and appends a compaction entry.
func (runtime *Runtime) CompactSession(
	ctx context.Context,
	sessionID string,
	cwd string,
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
	parentID, branch, err := runtime.compactionBranch(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	plan, err := planCompaction(branch, runtime.cfg.Context.KeepRecentTokens)
	if err != nil {
		return nil, err
	}

	summary, err := runtime.summarizeCompaction(ctx, cwd, sessionID, selectedModel, auth, plan.Messages)
	if err != nil {
		return nil, err
	}

	return runtime.appendCompaction(ctx, sessionID, parentID, summary, &plan)
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
) (*string, []database.EntryEntity, error) {
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

func (runtime *Runtime) appendCompaction(
	ctx context.Context,
	sessionID string,
	parentID *string,
	summary string,
	plan *compactionPlan,
) (*database.EntryEntity, error) {
	details := map[string]any{
		"summarized_entries": len(plan.SummarizedEntryIDs),
		"kept_entries":       len(plan.KeptEntryIDs),
		"tokens_before":      plan.TokensBefore,
	}
	entry, err := runtime.sessions.AppendCompaction(
		ctx,
		sessionID,
		parentID,
		summary,
		plan.FirstKeptEntryID,
		plan.TokensBefore,
		details,
		false,
	)
	if err != nil {
		return nil, oops.In("assistant").Code("append_compaction").Wrapf(err, "append compaction")
	}
	runtime.dispatchMessageAppend(ctx, entry)

	return entry, nil
}

type compactionPlan struct {
	FirstKeptEntryID   string
	Messages           []database.MessageEntity
	SummarizedEntryIDs []string
	KeptEntryIDs       []string
	TokensBefore       int
}

func planCompaction(branch []database.EntryEntity, keepRecentTokens int) (compactionPlan, error) {
	modelFacing := modelFacingBranchEntries(branch)
	if len(modelFacing) < 2 {
		return compactionPlan{}, oops.In("assistant").
			Code("compact_nothing_to_do").
			Errorf("not enough model-facing history to compact")
	}
	if keepRecentTokens <= 0 {
		keepRecentTokens = defaultCompactionKeepRecentTokens
	}

	firstKept := firstKeptEntryIndex(modelFacing, keepRecentTokens)
	if firstKept <= 0 {
		return compactionPlan{}, oops.In("assistant").
			Code("compact_nothing_to_do").
			Errorf("not enough old history to compact while preserving the recent tail")
	}

	summarized := modelFacing[:firstKept]
	kept := modelFacing[firstKept:]
	messages := make([]database.MessageEntity, 0, len(summarized))
	summarizedIDs := make([]string, 0, len(summarized))
	keptIDs := make([]string, 0, len(kept))
	for index := range summarized {
		messages = append(messages, modelFacingMessage(&summarized[index].Message))
		summarizedIDs = append(summarizedIDs, summarized[index].ID)
	}
	for index := range kept {
		keptIDs = append(keptIDs, kept[index].ID)
	}

	return compactionPlan{
		Messages:           messages,
		SummarizedEntryIDs: summarizedIDs,
		KeptEntryIDs:       keptIDs,
		FirstKeptEntryID:   kept[0].ID,
		TokensBefore:       estimateMessageTokens(messages),
	}, nil
}

func modelFacingBranchEntries(branch []database.EntryEntity) []database.EntryEntity {
	entries := make([]database.EntryEntity, 0, len(branch))
	for index := range branch {
		entry := branch[index]
		if !isCompactionCandidateEntry(entry.Type) {
			continue
		}
		message := compactionCandidateMessage(&entry)
		if !entry.ModelFacing || !isModelFacingRole(message.Role) || strings.TrimSpace(message.Content) == "" {
			continue
		}
		entry.Message = message
		entries = append(entries, entry)
	}

	return entries
}

func isCompactionCandidateEntry(entryType database.EntryType) bool {
	switch entryType {
	case database.EntryTypeMessage,
		database.EntryTypeCustomMessage,
		database.EntryTypeBranchSummary,
		database.EntryTypeCompaction:
		return true
	case database.EntryTypeCustom,
		database.EntryTypeLabel,
		database.EntryTypeModelChange,
		database.EntryTypeSessionInfo,
		database.EntryTypeThinkingLevelChange:
		return false
	}

	return false
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

func firstKeptEntryIndex(entries []database.EntryEntity, keepRecentTokens int) int {
	lastIndex := len(entries) - 1
	tokens := 0
	for index := lastIndex; index >= 0; index-- {
		message := modelFacingMessage(&entries[index].Message)
		tokens += estimateTokens(message.Content)
		if index == lastIndex {
			continue
		}
		if tokens > keepRecentTokens {
			return index + 1
		}
	}

	return 0
}

func (runtime *Runtime) summarizeCompaction(
	ctx context.Context,
	cwd string,
	sessionID string,
	selectedModel *model.Model,
	auth model.RequestAuth,
	messages []database.MessageEntity,
) (string, error) {
	request := &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: runtime.emitProviderRequest,
		OnProviderRequest: runtime.dispatchProviderRequestHook,
		OnToolCall:        nil,
		OnToolResult:      nil,
		ToolRegistry:      tool.NewRegistry(cwd),
		DisableTools:      true,
		SessionID:         sessionID,
		SystemPrompt:      compactionSummaryPrompt,
		ThinkingLevel:     thinkingOff,
		CWD:               cwd,
		Auth:              auth,
		Messages:          messages,
		Usage:             compactionRequestUsage(selectedModel, messages),
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

	return summary, nil
}

func compactionRequestUsage(selectedModel *model.Model, messages []database.MessageEntity) model.TokenUsage {
	contextWindow := 0
	if selectedModel != nil {
		contextWindow = selectedModel.ContextWindow
	}
	inputTokens := estimateInputTokens(compactionSummaryPrompt, messages)

	return model.TokenUsage{
		Breakdown: map[string]int{
			jsonSystemRole:          estimateTokens(compactionSummaryPrompt),
			contextBreakdownHistory: estimateMessageTokens(messages),
		},
		TopContributors: nil,
		ContextWindow:   contextWindow,
		ContextTokens:   inputTokens,
		InputTokens:     inputTokens,
		OutputTokens:    0,
	}
}
