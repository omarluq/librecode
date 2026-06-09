// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
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
	plan, err := compaction.PlanBranch(branch, runtime.cfg.Context.KeepRecentTokens, contextwindow.EstimateTokens)
	if err != nil {
		return nil, err
	}
	plan.FileOperations = compaction.CollectFileOperations(branch[:plan.FirstKeptEntryIndex])

	return runtime.compactSessionWithPlan(ctx, sessionID, cwd, parentID, selectedModel, auth, &plan)
}

func (runtime *Runtime) compactSessionWithPlan(
	ctx context.Context,
	sessionID string,
	cwd string,
	parentID *string,
	selectedModel *model.Model,
	auth model.RequestAuth,
	plan *compaction.Plan,
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
	plan *compaction.Plan,
) (summary string, decision *compactionLifecycleDecision, fromHook bool, err error) {
	decision, err = runtime.dispatchBeforeCompaction(ctx, sessionID, cwd, plan)
	if errors.Is(err, errNoCompactionDecision) {
		decision = nil
	} else if err != nil {
		return "", nil, false, err
	}
	if decision != nil && decision.Summary != "" {
		return compaction.AppendFileOperationsSummary(decision.Summary, plan.FileOperations), decision, true, nil
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
	plan *compaction.Plan,
	decision *compactionLifecycleDecision,
	fromHook bool,
) (*database.EntryEntity, error) {
	details := map[string]any{
		"summarized_entries":             len(plan.SummarizedEntryIDs),
		"kept_entries":                   len(plan.KeptEntryIDs),
		lifecyclepayload.TokensBeforeKey: plan.TokensBefore,
	}
	if decision != nil {
		maps.Copy(details, decision.Details)
	}
	if len(plan.FileOperations) > 0 {
		details[compaction.FileOperationsKey] = plan.FileOperations
	}
	entry, err := runtime.sessions.AppendCompaction(ctx, &database.AppendCompactionInput{
		ParentID:         parentID,
		Details:          details,
		SessionID:        sessionID,
		Summary:          summary,
		FirstKeptEntryID: plan.FirstKeptEntryID,
		TokensBefore:     plan.TokensBefore,
		FromHook:         fromHook,
	})
	if err != nil {
		return nil, oops.In("assistant").Code("append_compaction").Wrapf(err, "append compaction")
	}
	runtime.dispatchMessageAppend(ctx, entry)

	return entry, nil
}

func (runtime *Runtime) summarizeCompaction(
	ctx context.Context,
	cwd string,
	sessionID string,
	selectedModel *model.Model,
	auth model.RequestAuth,
	plan *compaction.Plan,
) (string, error) {
	systemPrompt := compaction.SystemPrompt(plan.PreviousSummary, plan.SplitTurnSummary)
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

	return compaction.AppendFileOperationsSummary(summary, plan.FileOperations), nil
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
	inputTokens := contextwindow.EstimateInputTokens(systemPrompt, messages)

	return model.TokenUsage{
		Breakdown: map[string]int{
			jsonSystemRole:                 contextwindow.EstimateTokens(systemPrompt),
			contextwindow.BreakdownHistory: contextwindow.EstimateMessageTokens(messages),
		},
		TopContributors: nil,
		ContextWindow:   contextWindow,
		ContextTokens:   inputTokens,
		InputTokens:     inputTokens,
		OutputTokens:    0,
	}
}
