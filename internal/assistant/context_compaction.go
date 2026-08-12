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

	releaseOperation, err := runtime.operations.acquire(ctx, sessionID)
	if err != nil {
		return nil, oops.In("assistant").Code("compact_operation_wait").Wrapf(err, "wait for session operation")
	}
	defer releaseOperation()

	return runtime.compactSessionFrom(ctx, sessionID, cwd, parentEntryID)
}

func (runtime *Runtime) compactSessionFrom(
	ctx context.Context,
	sessionID string,
	cwd string,
	parentEntryID *string,
) (*database.EntryEntity, error) {
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

	currentTokens := compaction.BranchTokens(branch, contextwindow.EstimateTokens)
	recentTailTokens := runtime.compactionRecentTailTokens(selectedModel, currentTokens)

	plan, err := compaction.PlanBranch(branch, recentTailTokens, contextwindow.EstimateTokens)
	if err != nil {
		return nil, assistantError(err, "plan compaction")
	}

	plan.FileOperations = compaction.CollectFileOperations(branch[:plan.FirstKeptEntryIndex])

	operationID, err := runtime.compactionOperationID()
	if err != nil {
		return nil, err
	}

	providerInput := compactionProviderInput{
		selectedModel: selectedModel,
		auth:          auth,
		operationID:   operationID,
	}

	return runtime.compactSessionWithPlan(ctx, sessionID, cwd, parentID, branch, providerInput, &plan)
}

func (runtime *Runtime) compactionOperationID() (string, error) {
	operationUUID, err := runtime.newCompactionUUID()
	if err != nil {
		return "", oops.In("assistant").Code("compact_operation_id").Wrapf(err, "generate compaction operation id")
	}

	return operationUUID.String(), nil
}

type compactionProviderInput struct {
	selectedModel *model.Model
	operationID   string
	auth          model.RequestAuth
}

type compactionAppendInput struct {
	sessionID   string
	parentID    *string
	summary     string
	plan        *compaction.Plan
	decision    *compactionLifecycleDecision
	operationID string
	fromHook    bool
}

func (runtime *Runtime) compactionRecentTailTokens(selectedModel *model.Model, currentTokens int) int {
	contextWindow := 0
	if selectedModel != nil {
		contextWindow = selectedModel.ContextWindow
	}

	return contextwindow.RecentTailTarget(contextwindow.RecentTailInput{
		ContextWindow: contextWindow,
		CurrentTokens: currentTokens,
	})
}

func (runtime *Runtime) compactSessionWithPlan(
	ctx context.Context,
	sessionID string,
	cwd string,
	parentID *string,
	branch []database.EntryEntity,
	providerInput compactionProviderInput,
	plan *compaction.Plan,
) (*database.EntryEntity, error) {
	decision, err := runtime.dispatchBeforeCompaction(ctx, sessionID, cwd, plan)
	if errors.Is(err, errNoCompactionDecision) {
		decision = nil
	} else if err != nil {
		return nil, err
	}

	if decision != nil && decision.FirstKeptEntryID != "" {
		adjustedPlan, planErr := compaction.PlanBranchFromFirstKept(
			branch,
			decision.FirstKeptEntryID,
			contextwindow.EstimateTokens,
		)
		if planErr != nil {
			return nil, assistantError(planErr, "replan compaction")
		}

		plan = &adjustedPlan
	}

	plan.FileOperations = compaction.CollectFileOperations(branch[:plan.FirstKeptEntryIndex])

	summary, fromHook, err := runtime.compactionSummary(
		ctx,
		cwd,
		sessionID,
		providerInput.selectedModel,
		providerInput.auth,
		plan,
		decision,
	)
	if err != nil {
		return nil, err
	}

	entry, err := runtime.appendCompaction(ctx, &compactionAppendInput{
		sessionID:   sessionID,
		parentID:    parentID,
		summary:     summary,
		plan:        plan,
		decision:    decision,
		fromHook:    fromHook,
		operationID: providerInput.operationID,
	})
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

func (runtime *Runtime) compactionSummary(
	ctx context.Context,
	cwd string,
	sessionID string,
	selectedModel *model.Model,
	auth model.RequestAuth,
	plan *compaction.Plan,
	decision *compactionLifecycleDecision,
) (summary string, fromHook bool, err error) {
	if decision != nil && decision.Summary != "" {
		return compaction.AppendFileOperationsSummary(decision.Summary, plan.FileOperations), true, nil
	}

	summary, err = runtime.summarizeCompaction(ctx, cwd, sessionID, selectedModel, auth, plan)
	if err != nil {
		return "", false, err
	}

	return summary, false, nil
}

func (runtime *Runtime) appendCompaction(
	ctx context.Context,
	input *compactionAppendInput,
) (*database.EntryEntity, error) {
	details := map[string]any{
		"summarized_entries":             len(input.plan.SummarizedEntryIDs),
		"kept_entries":                   len(input.plan.KeptEntryIDs),
		lifecyclepayload.TokensBeforeKey: input.plan.TokensBefore,
	}
	if input.decision != nil {
		maps.Copy(details, input.decision.Details)
	}

	if len(input.plan.FileOperations) > 0 {
		details[compaction.FileOperationsKey] = input.plan.FileOperations
	}

	entry, err := runtime.sessions.AppendCompaction(ctx, &database.AppendCompactionInput{
		ParentID:         input.parentID,
		Details:          details,
		SessionID:        input.sessionID,
		Summary:          input.summary,
		FirstKeptEntryID: input.plan.FirstKeptEntryID,
		TokensBefore:     input.plan.TokensBefore,
		FromHook:         input.fromHook,
		OperationID:      input.operationID,
	})
	if err != nil {
		if errors.Is(err, database.ErrStaleCompactionParent) {
			return nil, oops.In("assistant").Code("compact_stale_parent").Wrapf(err, "append compaction")
		}

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
		OnEvent:                nil,
		OnProviderObserve:      runtime.emitProviderRequest,
		OnProviderResponse:     observeProviderUsage,
		OnRoundCheckpoint:      nil,
		OnProviderRequest:      runtime.dispatchProviderRequestHook,
		ToolRegistry:           tool.NewRegistry(cwd),
		ExecuteTools:           nil,
		DisableTools:           true,
		ToolSideEffectsStarted: false,
		SessionID:              sessionID,
		SystemPrompt:           systemPrompt,
		ThinkingLevel:          thinkingOff,
		CWD:                    cwd,
		Auth:                   auth,
		Messages:               plan.Messages,
		Usage:                  compactionRequestUsage(selectedModel, systemPrompt, plan.Messages),
		Model:                  *selectedModel,
		ProviderAttempt:        0,
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
