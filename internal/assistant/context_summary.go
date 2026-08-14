package assistant

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
)

const defaultUnknownSummaryContextWindow = 64_000

type summaryRequest struct {
	completion *CompletionRequest
	budget     contextwindow.SummaryBudget
}

func (runtime *Runtime) summaryOutputLimit(selectedModel *model.Model) (int, error) {
	contextWindow, modelMax := summaryModelLimits(selectedModel)
	reserve := contextwindow.OutputReserve(selectedModel, contextWindow, &runtime.cfg.Context)

	limit, err := contextwindow.SummaryOutputLimit(reserve, modelMax)
	if err != nil {
		return 0, oops.In("assistant").Code("compact_summary_output_limit").Wrapf(err, "compute summary output limit")
	}

	if configured := runtime.cfg.Context.SummaryOutputTokens; configured > 0 {
		limit = min(limit, configured)
	}

	return limit, nil
}

func summaryModelLimits(selectedModel *model.Model) (contextWindow, modelMax int) {
	if selectedModel != nil {
		contextWindow = selectedModel.ContextWindow
		modelMax = selectedModel.MaxTokens
	}
	// Invalid/unknown catalog metadata uses a finite conservative fallback so
	// custom catalogs cannot create an unbounded summary request.
	if contextWindow < contextwindow.MinimumSummaryOutputTokens {
		contextWindow = defaultUnknownSummaryContextWindow
	}

	if modelMax < 0 {
		modelMax = 0
	}

	return contextWindow, modelMax
}

func (runtime *Runtime) completeSummary(
	ctx context.Context,
	request *summaryRequest,
	operation compaction.Operation,
) (compaction.SummaryOutcome, error) {
	outcome, err := runtime.completeSummaryAttempt(ctx, request, operation)
	if err != nil {
		return outcome, err
	}

	if err := compaction.ValidateCheckpoint(outcome.Text); err == nil {
		return outcome, nil
	}

	repair := *request.completion
	repair.SystemPrompt = compaction.RepairPrompt()
	repair.Messages = []database.MessageEntity{{
		Timestamp: time.Time{}, Role: database.RoleUser, Content: outcome.Text,
		Provider: "", Model: "", Parts: nil,
	}}
	repair.Usage = compactionRequestUsage(&repair.Model, repair.SystemPrompt, repair.Messages)
	repairRequest := &summaryRequest{
		completion: &repair,
		budget: contextwindow.NewSummaryBudget(contextwindow.SummaryBudgetInput{
			ContextWindow:         request.budget.ContextWindow,
			SystemPromptTokens:    contextwindow.EstimateTokens(repair.SystemPrompt),
			PreviousSummaryTokens: 0,
			SplitTurnTokens:       0,
			HistoryTokens:         contextwindow.EstimateMessageTokens(repair.Messages),
			ProviderReserve:       request.budget.ProviderReserve,
			SafetyMargin:          request.budget.SafetyMargin,
			EnvelopeTokens:        request.budget.EnvelopeTokens,
			MaxOutputTokens:       repair.MaxTokens,
		}),
	}

	repaired, repairErr := runtime.completeSummaryAttempt(ctx, repairRequest, operation)
	if repairErr != nil {
		return repaired, repairErr
	}

	if structureErr := compaction.ValidateCheckpoint(repaired.Text); structureErr != nil {
		return repaired, oops.In("assistant").Code("compact_summary_structure").Wrapf(
			structureErr, "validate repaired compaction checkpoint",
		)
	}

	return repaired, nil
}

func (runtime *Runtime) completeSummaryAttempt(
	ctx context.Context,
	request *summaryRequest,
	operation compaction.Operation,
) (compaction.SummaryOutcome, error) {
	outcome := compaction.SummaryOutcome{
		Text:                 "",
		Provider:             request.completion.Model.Provider,
		Model:                request.completion.Model.ID,
		Reason:               operation.Reason,
		FinishReason:         llm.FinishReasonUnknown,
		EstimatedInputTokens: request.budget.TotalInputTokens,
		ReportedInputTokens:  0,
		ReportedOutputTokens: 0,
		OutputLimit:          request.completion.MaxTokens,
		Truncated:            false,
	}

	if err := request.budget.Validate(); err != nil {
		kind := compaction.ErrSummaryInputOverflow
		cause := err

		if request.budget.ContextWindow <= 0 || request.budget.MaxOutputTokens <= 0 ||
			request.budget.MaxInputTokens <= 0 || request.budget.FixedInputTokens > request.budget.MaxInputTokens {
			kind = compaction.ErrSummaryFixedOverhead
			cause = errors.Join(compaction.ErrSummaryInputOverflow, err)
		}

		return outcome, &compaction.SummaryError{
			Kind: kind, Cause: cause, Provider: outcome.Provider, Model: outcome.Model, Reason: outcome.Reason,
			Input: request.budget.TotalInputTokens, Limit: request.budget.MaxInputTokens, Before: 0, After: 0,
		}
	}

	result, err := runtime.completeWithRetry(ctx, request.completion, nil, nil)
	if err != nil {
		return outcome, oops.In("assistant").Code("compact_summarize").Wrapf(err, "summarize compacted context")
	}

	outcome.Text = strings.TrimSpace(result.Text)
	outcome.FinishReason = result.FinishReason
	outcome.ReportedInputTokens = result.Usage.InputTokens
	outcome.ReportedOutputTokens = result.Usage.OutputTokens
	outcome.Truncated = summaryWasTruncated(result)

	if validationErr := validateSummaryOutcome(&outcome, result); validationErr != nil {
		return outcome, validationErr
	}

	return outcome, nil
}

func summaryWasTruncated(result *CompletionResult) bool {
	return result.FinishReason == llm.FinishReasonLength ||
		result.Termination.IncompleteReason == "max_output_tokens" ||
		result.Termination.ProviderFinishReason == "max_tokens"
}

func validateSummaryOutcome(outcome *compaction.SummaryOutcome, result *CompletionResult) error {
	input := outcome.EstimatedInputTokens

	var kind error

	switch {
	case outcome.Truncated:
		kind = compaction.ErrSummaryOutputTruncated
	case outcome.FinishReason == llm.FinishReasonContentFilter:
		kind = compaction.ErrSummaryContentFilter
	case outcome.FinishReason == llm.FinishReasonRefusal:
		kind = compaction.ErrSummaryRefusal
	case outcome.FinishReason == llm.FinishReasonAborted:
		kind = compaction.ErrSummaryAborted
	case outcome.FinishReason == llm.FinishReasonError || summaryHasContextOverflow(result):
		kind = compaction.ErrSummaryProviderFailure
	case outcome.Text == "":
		kind = compaction.ErrSummaryEmpty
	case contextwindow.EstimateTokens(outcome.Text) > outcome.OutputLimit:
		kind = compaction.ErrSummaryOutputTruncated
		input = contextwindow.EstimateTokens(outcome.Text)
	default:
		return nil
	}

	return &compaction.SummaryError{
		Kind: kind, Cause: nil, Provider: outcome.Provider, Model: outcome.Model, Reason: outcome.Reason,
		Input: input, Limit: outcome.OutputLimit, Before: 0, After: 0,
	}
}

func summaryHasContextOverflow(result *CompletionResult) bool {
	return result.Termination.ProviderFinishReason == "model_context_window_exceeded"
}
