// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	pendingCompactionOperationID = "pending"
	summaryRetryReductionDivisor = 2
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

	operationID, err := runtime.compactionOperationID()
	if err != nil {
		return nil, err
	}

	return runtime.compactSessionFrom(ctx, sessionID, cwd, parentEntryID, compaction.Operation{
		ID:          operationID,
		Reason:      compaction.ReasonManual,
		RetryIntent: compaction.RetryNone,
	})
}

func (runtime *Runtime) compactSessionFrom(
	ctx context.Context,
	sessionID string,
	cwd string,
	parentEntryID *string,
	operation compaction.Operation,
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

	if operation.ID == pendingCompactionOperationID {
		operationID, operationErr := runtime.compactionOperationID()
		if operationErr != nil {
			return nil, operationErr
		}

		operation.ID = operationID
	}

	validateErr := operation.Validate()
	if validateErr != nil {
		return nil, oops.In("assistant").Code("compact_operation").Wrapf(validateErr, "validate compaction operation")
	}

	outputLimit, err := runtime.summaryOutputLimit(selectedModel)
	if err != nil {
		return nil, err
	}

	providerInput := compactionProviderInput{
		selectedModel: selectedModel,
		operation:     operation,
		auth:          auth,
		outputLimit:   outputLimit,
	}

	return runtime.compactSessionWithPlan(ctx, sessionID, cwd, parentID, branch, &providerInput, &plan)
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
	operation     compaction.Operation
	auth          model.RequestAuth
	outputLimit   int
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

type compactionSummaryInput struct {
	providerInput *compactionProviderInput
	plan          *compaction.Plan
	decision      *compactionLifecycleDecision
	cwd           string
	sessionID     string
}

func (runtime *Runtime) compactionRecentTailTokens(selectedModel *model.Model, currentTokens int) int {
	contextWindow := 0
	if selectedModel != nil {
		contextWindow = selectedModel.ContextWindow
	}

	target := contextwindow.RecentTailTarget(contextwindow.RecentTailInput{
		ContextWindow: contextWindow,
		CurrentTokens: currentTokens,
	})
	if configured := runtime.cfg.Context.RetainedTailMaxTokens; configured > 0 {
		target = min(target, configured)
	}

	return target
}

func (runtime *Runtime) compactSessionWithPlan(
	ctx context.Context,
	sessionID string,
	cwd string,
	parentID *string,
	branch []database.EntryEntity,
	providerInput *compactionProviderInput,
	plan *compaction.Plan,
) (*database.EntryEntity, error) {
	decision, err := runtime.dispatchBeforeCompaction(ctx, sessionID, cwd, plan, providerInput.operation)
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

	compactedEntries := branch[:plan.FirstKeptEntryIndex]
	plan.FileOperations = compaction.CollectFileOperations(compactedEntries)
	plan.ValidationRecords = compaction.CollectValidationRecords(compactedEntries)
	plan.ActiveWorkRecords = compaction.CollectActiveWorkRecords(compactedEntries)

	summary, fromHook, err := runtime.compactionSummary(ctx, &compactionSummaryInput{
		cwd:           cwd,
		sessionID:     sessionID,
		providerInput: providerInput,
		plan:          plan,
		decision:      decision,
	})
	if err != nil {
		return nil, err
	}

	if structureErr := compaction.ValidateCheckpoint(compaction.StripDeterministicState(summary)); structureErr != nil {
		return nil, oops.In("assistant").Code("compact_summary_structure").Wrapf(
			structureErr, "validate compaction checkpoint",
		)
	}

	validationErr := runtime.validateCompactionResult(ctx, cwd, summary, branch, plan, providerInput)
	if validationErr != nil {
		return nil, validationErr
	}

	entry, err := runtime.appendCompaction(ctx, &compactionAppendInput{
		sessionID:   sessionID,
		parentID:    parentID,
		summary:     summary,
		plan:        plan,
		decision:    decision,
		fromHook:    fromHook,
		operationID: providerInput.operation.ID,
	})
	if err != nil {
		return nil, err
	}

	runtime.dispatchAfterCompaction(ctx, sessionID, cwd, entry, plan, providerInput.operation, fromHook)

	return entry, nil
}

func (runtime *Runtime) compactionModelAuth(ctx context.Context) (*model.Model, model.RequestAuth, error) {
	selectedModel, err := runtime.compactionModel()
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

func (runtime *Runtime) compactionModel() (model.Model, error) {
	provider := runtime.cfg.Context.CompactionProvider

	modelID := runtime.cfg.Context.CompactionModel
	if provider == "" && modelID == "" {
		return runtime.selectedModel()
	}

	candidates := runtime.models.All()
	for index := range candidates {
		if candidates[index].Provider == provider && candidates[index].ID == modelID {
			return candidates[index], nil
		}
	}

	return model.Model{}, oops.In("assistant").Code("compaction_model_unavailable").Errorf(
		"configured compaction model %s/%s is unavailable",
		provider,
		modelID,
	)
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
	input *compactionSummaryInput,
) (summary string, fromHook bool, err error) {
	if input.decision != nil && input.decision.Summary != "" {
		return compaction.AppendDeterministicState(
			input.decision.Summary,
			input.plan.FileOperations,
			input.plan.ValidationRecords,
			input.plan.ActiveWorkRecords,
		), true, nil
	}

	summary, err = runtime.summarizeCompaction(ctx, input)
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

	if len(input.plan.ValidationRecords) > 0 {
		details["validation_records"] = input.plan.ValidationRecords
	}

	if len(input.plan.ActiveWorkRecords) > 0 {
		details["active_work_records"] = input.plan.ActiveWorkRecords
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
	input *compactionSummaryInput,
) (string, error) {
	plan := input.plan
	providerInput := input.providerInput
	systemPrompt := compaction.SystemPrompt(plan.PreviousSummary, plan.SplitTurnSummary)
	request := &CompletionRequest{
		OnEvent:                nil,
		OnProviderObserve:      runtime.emitProviderRequest,
		OnProviderResponse:     providerUsageObserver(),
		OnRoundCheckpoint:      nil,
		OnProviderRequest:      runtime.dispatchProviderRequestHook,
		ToolRegistry:           tool.NewRegistry(input.cwd),
		ExecuteTools:           nil,
		DisableTools:           true,
		ToolSideEffectsStarted: false,
		SessionID:              input.sessionID,
		Identity:               emptyRequestIdentity(),
		SystemPrompt:           systemPrompt,
		ThinkingLevel:          thinkingOff,
		CWD:                    input.cwd,
		Auth:                   providerInput.auth,
		Messages:               plan.Messages,
		Usage:                  compactionRequestUsage(providerInput.selectedModel, systemPrompt, plan.Messages),
		Model:                  *providerInput.selectedModel,
		ProviderAttempt:        0,
		MaxTokens:              providerInput.outputLimit,
	}

	outcome, err := runtime.completeSummary(ctx, runtime.newSummaryRequest(request), providerInput.operation)
	if errors.Is(err, compaction.ErrSummaryOutputTruncated) {
		initialErr := err

		outcome, err = runtime.completeReducedSummary(ctx, request, plan.SummaryGroups, providerInput.operation)
		if err != nil {
			return "", errors.Join(initialErr, err)
		}
	}

	if err != nil {
		return "", err
	}

	return compaction.AppendDeterministicState(
		outcome.Text, plan.FileOperations, plan.ValidationRecords, plan.ActiveWorkRecords,
	), nil
}

func (runtime *Runtime) newSummaryRequest(request *CompletionRequest) *summaryRequest {
	contextWindow, _ := summaryModelLimits(&request.Model)

	return &summaryRequest{
		completion: request,
		budget: contextwindow.NewSummaryBudget(contextwindow.SummaryBudgetInput{
			ContextWindow:         contextWindow,
			SystemPromptTokens:    contextwindow.EstimateTokens(request.SystemPrompt),
			PreviousSummaryTokens: 0,
			SplitTurnTokens:       0,
			HistoryTokens:         contextwindow.EstimateMessageTokens(request.Messages),
			ProviderReserve:       max(runtime.cfg.Context.ProviderReserveTokens, 0),
			SafetyMargin:          max(runtime.cfg.Context.SafetyMarginTokens, 0),
			EnvelopeTokens:        contextwindow.DefaultSummaryRequestEnvelopeTokens,
			MaxOutputTokens:       request.MaxTokens,
		}),
	}
}

func (runtime *Runtime) completeReducedSummary(
	ctx context.Context,
	base *CompletionRequest,
	groups []compaction.SemanticGroup,
	operation compaction.Operation,
) (compaction.SummaryOutcome, error) {
	return runtime.completeReducedSummaryLevel(ctx, base, groups, operation, 1, true)
}

func (runtime *Runtime) completeReducedSummaryLevel(
	ctx context.Context,
	base *CompletionRequest,
	groups []compaction.SemanticGroup,
	operation compaction.Operation,
	depth int,
	forceSplit bool,
) (compaction.SummaryOutcome, error) {
	requestBudget := runtime.newSummaryRequest(base).budget
	availableTokens := requestBudget.AvailableHistoryTokens()
	beforeTokens := contextwindow.EstimateMessageTokens(base.Messages)
	chunkLimit := availableTokens

	if depth > compaction.MaxReductionDepth {
		return compaction.SummaryOutcome{}, reductionNoProgressError(
			base, operation, beforeTokens, chunkLimit, beforeTokens, nil,
		)
	}

	chunks, err := partitionReductionGroups(groups, chunkLimit, forceSplit)
	if err != nil {
		return compaction.SummaryOutcome{}, reductionNoProgressError(
			base, operation, beforeTokens, chunkLimit, beforeTokens, err,
		)
	}

	reducedGroups, outcome, err := runtime.summarizeReductionChunks(ctx, base, chunks, operation)
	if err != nil {
		return outcome, err
	}

	reducedMessages := messagesFromSemanticGroups(reducedGroups)
	afterTokens := contextwindow.EstimateMessageTokens(reducedMessages)

	if afterTokens >= beforeTokens {
		return compaction.SummaryOutcome{}, reductionNoProgressError(
			base, operation, beforeTokens, chunkLimit, afterTokens, nil,
		)
	}

	request := *base
	request.Messages = reducedMessages
	request.Usage = compactionRequestUsage(&request.Model, request.SystemPrompt, request.Messages)

	if err := runtime.newSummaryRequest(&request).budget.Validate(); err == nil {
		return runtime.completeSummary(ctx, runtime.newSummaryRequest(&request), operation)
	}

	return runtime.completeReducedSummaryLevel(
		ctx, &request, reducedGroups, operation, depth+1, false,
	)
}

func partitionReductionGroups(
	groups []compaction.SemanticGroup,
	chunkLimit int,
	forceSplit bool,
) ([]compaction.Chunk, error) {
	chunks, err := compaction.Partition(groups, chunkLimit, compaction.MaxChunksPerReductionRound)
	if err != nil {
		return nil, fmt.Errorf("partition summary reduction groups: %w", err)
	}

	if forceSplit && len(chunks) == 1 && len(groups) >= summaryRetryReductionDivisor {
		split := len(groups) / summaryRetryReductionDivisor
		chunks = []compaction.Chunk{
			chunkFromSemanticGroups(groups[:split]),
			chunkFromSemanticGroups(groups[split:]),
		}
	}

	if len(chunks) < summaryRetryReductionDivisor {
		return nil, compaction.ErrSummaryReductionNoProgress
	}

	return chunks, nil
}

func (runtime *Runtime) summarizeReductionChunks(
	ctx context.Context,
	base *CompletionRequest,
	chunks []compaction.Chunk,
	operation compaction.Operation,
) ([]compaction.SemanticGroup, compaction.SummaryOutcome, error) {
	reducedGroups := make([]compaction.SemanticGroup, 0, len(chunks))
	for index := range chunks {
		request := *base
		request.Messages = chunks[index].Messages
		request.Usage = compactionRequestUsage(&request.Model, request.SystemPrompt, request.Messages)

		outcome, err := runtime.completeSummary(ctx, runtime.newSummaryRequest(&request), operation)
		if err != nil {
			return nil, outcome, err
		}

		message := database.MessageEntity{
			Timestamp: time.Time{}, Role: database.RoleUser, Content: outcome.Text,
			Provider: "", Model: "", Parts: nil,
		}
		reducedGroups = append(reducedGroups, compaction.SemanticGroup{
			Kind:     compaction.SemanticGroupReduction,
			EntryIDs: nil,
			Messages: []database.MessageEntity{message},
			Tokens:   contextwindow.EstimateMessageTokens([]database.MessageEntity{message}),
		})
	}

	return reducedGroups, emptySummaryOutcome(), nil
}

func emptySummaryOutcome() compaction.SummaryOutcome {
	return compaction.SummaryOutcome{
		Text: "", Provider: "", Model: "", Reason: "", FinishReason: llm.FinishReasonUnknown,
		EstimatedInputTokens: 0, ReportedInputTokens: 0, ReportedOutputTokens: 0,
		OutputLimit: 0, Truncated: false,
	}
}

func chunkFromSemanticGroups(groups []compaction.SemanticGroup) compaction.Chunk {
	messages := messagesFromSemanticGroups(groups)

	return compaction.Chunk{
		Groups: groups, Messages: messages, Tokens: contextwindow.EstimateMessageTokens(messages),
	}
}

func messagesFromSemanticGroups(groups []compaction.SemanticGroup) []database.MessageEntity {
	messageCount := 0
	for index := range groups {
		messageCount += len(groups[index].Messages)
	}

	messages := make([]database.MessageEntity, 0, messageCount)
	for index := range groups {
		messages = append(messages, groups[index].Messages...)
	}

	return messages
}

func reductionNoProgressError(
	base *CompletionRequest,
	operation compaction.Operation,
	input, limit, after int,
	cause error,
) error {
	return &compaction.SummaryError{
		Kind: compaction.ErrSummaryReductionNoProgress, Cause: cause,
		Provider: base.Model.Provider, Model: base.Model.ID, Reason: operation.Reason,
		Input: input, Limit: limit, Before: input, After: after,
	}
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
		Provenance:      "",
		TopContributors: nil,
		ContextWindow:   contextWindow,
		ContextTokens:   inputTokens,
		InputTokens:     inputTokens,
		OutputTokens:    0,
	}
}
