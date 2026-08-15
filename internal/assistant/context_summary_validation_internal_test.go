package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
)

const (
	summaryValidationModelID  = "summary-validation-model"
	summaryValidationProvider = "provider"
	malformedSummary          = "malformed"
)

type summaryValidationCompleter struct {
	responses     []string
	finishReasons []llm.FinishReason
	calls         int
}

func (client *summaryValidationCompleter) Complete(
	_ context.Context,
	_ *CompletionRequest,
) (*CompletionResult, error) {
	client.calls++

	text := summaryValidationCheckpoint("summary")
	if client.calls <= len(client.responses) {
		text = client.responses[client.calls-1]
	}

	finishReason := llm.FinishReasonStop
	if client.calls <= len(client.finishReasons) {
		finishReason = client.finishReasons[client.calls-1]
	}

	return &CompletionResult{
		FinishReason: finishReason,
		Termination:  llm.NewTerminationMetadata("", "", ""),
		Text:         text,
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        model.EmptyTokenUsage(),
	}, nil
}

func TestCompleteSummaryValidatesInputBeforeProviderDispatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		budget            contextwindow.SummaryBudgetInput
		wantFixedOverhead bool
	}{
		{
			name: "reducible history overflow",
			budget: contextwindow.SummaryBudgetInput{
				ContextWindow: 200, SystemPromptTokens: 20, PreviousSummaryTokens: 0,
				SplitTurnTokens: 0, HistoryTokens: 131, ProviderReserve: 0,
				SafetyMargin: 0, EnvelopeTokens: 10, MaxOutputTokens: 40,
			},
			wantFixedOverhead: false,
		},
		{
			name: "previous summary fixed overhead overflow",
			budget: contextwindow.SummaryBudgetInput{
				ContextWindow: 100, SystemPromptTokens: 70, PreviousSummaryTokens: 65,
				SplitTurnTokens: 0, HistoryTokens: 0, ProviderReserve: 0,
				SafetyMargin: 0, EnvelopeTokens: 0, MaxOutputTokens: 40,
			},
			wantFixedOverhead: true,
		},
		{
			name: "split summary fixed overhead overflow",
			budget: contextwindow.SummaryBudgetInput{
				ContextWindow: 100, SystemPromptTokens: 70, PreviousSummaryTokens: 0,
				SplitTurnTokens: 65, HistoryTokens: 0, ProviderReserve: 0,
				SafetyMargin: 0, EnvelopeTokens: 0, MaxOutputTokens: 40,
			},
			wantFixedOverhead: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := new(summaryValidationCompleter)
			runtime := newRuntimeFromDeps(func(deps *runtimeDeps) { deps.Client = client })
			request := summaryValidationRequest(testCase.budget)

			_, err := runtime.completeSummary(context.Background(), request, compaction.Operation{
				ID: "summary-validation", Reason: compaction.ReasonManual, RetryIntent: compaction.RetryNone,
			})

			require.ErrorIs(t, err, compaction.ErrSummaryInputOverflow)

			if testCase.wantFixedOverhead {
				require.ErrorIs(t, err, compaction.ErrSummaryFixedOverhead)
			} else {
				require.NotErrorIs(t, err, compaction.ErrSummaryFixedOverhead)
			}

			var summaryError *compaction.SummaryError
			require.ErrorAs(t, err, &summaryError)
			assert.Equal(t, summaryValidationProvider, summaryError.Provider)
			assert.Equal(t, summaryValidationModelID, summaryError.Model)
			assert.Equal(t, compaction.ReasonManual, summaryError.Reason)
			assert.Zero(t, client.calls, "invalid summary requests must not reach the provider")
		})
	}
}

func TestCompleteSummaryAcceptsExactInputLimit(t *testing.T) {
	t.Parallel()

	client := new(summaryValidationCompleter)
	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) { deps.Client = client })
	request := summaryValidationRequest(contextwindow.SummaryBudgetInput{
		ContextWindow: 240, SystemPromptTokens: 20, PreviousSummaryTokens: 0,
		SplitTurnTokens: 0, HistoryTokens: 130, ProviderReserve: 0,
		SafetyMargin: 0, EnvelopeTokens: 10, MaxOutputTokens: 80,
	})

	outcome, err := runtime.completeSummary(context.Background(), request, compaction.Operation{
		ID: "summary-boundary", Reason: compaction.ReasonManual, RetryIntent: compaction.RetryNone,
	})

	require.NoError(t, err)
	assert.Equal(t, summaryValidationCheckpoint("summary"), outcome.Text)
	assert.Equal(t, 160, outcome.EstimatedInputTokens)
	assert.Equal(t, 1, client.calls)
}

func TestCompleteSummaryRepairsMalformedStructureOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		responses []string
		wantErr   bool
	}{
		{name: "valid first response", responses: []string{summaryValidationCheckpoint("valid")}, wantErr: false},
		{
			name: "repair succeeds", responses: []string{
				malformedSummary, summaryValidationCheckpoint("repaired"),
			}, wantErr: false,
		},
		{name: "repair malformed", responses: []string{malformedSummary, "still malformed"}, wantErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := &summaryValidationCompleter{
				responses: testCase.responses, finishReasons: nil, calls: 0,
			}
			runtime := newRuntimeFromDeps(func(deps *runtimeDeps) { deps.Client = client })
			request := summaryValidationRequest(summaryValidationBudget())

			_, err := runtime.completeSummary(t.Context(), request, compaction.Operation{
				ID: "repair", Reason: compaction.ReasonManual, RetryIntent: compaction.RetryNone,
			})
			if testCase.wantErr {
				require.ErrorIs(t, err, compaction.ErrCheckpointStructure)
				assert.Equal(t, 2, client.calls)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, len(testCase.responses), client.calls)
		})
	}
}

func TestCompleteSummaryKeepsProviderFailuresDistinctFromTruncation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want   error
		name   string
		finish llm.FinishReason
	}{
		{name: "content filter", finish: llm.FinishReasonContentFilter, want: compaction.ErrSummaryContentFilter},
		{name: "refusal", finish: llm.FinishReasonRefusal, want: compaction.ErrSummaryRefusal},
		{name: "provider error", finish: llm.FinishReasonError, want: compaction.ErrSummaryProviderFailure},
		{name: "aborted", finish: llm.FinishReasonAborted, want: compaction.ErrSummaryAborted},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := &summaryValidationCompleter{
				responses:     []string{summaryValidationCheckpoint("partial")},
				finishReasons: []llm.FinishReason{testCase.finish},
				calls:         0,
			}
			runtime := newRuntimeFromDeps(func(deps *runtimeDeps) { deps.Client = client })
			request := summaryValidationRequest(summaryValidationBudget())

			_, err := runtime.completeSummary(t.Context(), request, compaction.Operation{
				ID: "finish-reason", Reason: compaction.ReasonManual, RetryIntent: compaction.RetryNone,
			})

			require.ErrorIs(t, err, testCase.want)
			require.NotErrorIs(t, err, compaction.ErrSummaryOutputTruncated)
			assert.Equal(t, 1, client.calls)
		})
	}
}

func TestCompleteReducedSummaryRecursesUntilCombinedSummariesFit(t *testing.T) {
	t.Parallel()

	client := &recursiveReductionCompleter{requests: nil}
	cfg := testRuntimeConfig()
	cfg.Context.ProviderReserveTokens = 0
	cfg.Context.SafetyMarginTokens = 0
	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Client = client
		deps.Config = cfg
	})

	base := newZeroCompletionRequest(model.RequestAuth{Headers: nil, APIKey: "", Error: "", OK: false})
	base.Model.Provider = summaryValidationProvider
	base.Model.ID = summaryValidationModelID
	base.Model.ContextWindow = 1_600
	base.SystemPrompt = "Summarize the supplied checkpoints."
	base.MaxTokens = 500

	groups := make([]compaction.SemanticGroup, 0, 4)

	for range 4 {
		message := database.MessageEntity{
			Timestamp: time.Time{}, Role: database.RoleUser,
			Content: repeatedSummaryTokens("history", 380), Provider: "", Model: "", Parts: nil,
		}
		groups = append(groups, compaction.SemanticGroup{
			Kind: compaction.SemanticGroupHistoryTurn, EntryIDs: nil,
			Messages: []database.MessageEntity{message},
			Tokens:   contextwindow.EstimateMessageTokens([]database.MessageEntity{message}),
		})
		base.Messages = append(base.Messages, message)
	}

	base.Usage = compactionRequestUsage(&base.Model, base.SystemPrompt, base.Messages)

	outcome, err := runtime.completeReducedSummary(t.Context(), base, groups, compaction.Operation{
		ID: "recursive", Reason: compaction.ReasonManual, RetryIntent: compaction.RetryNone,
	})

	require.NoError(t, err)
	assert.Equal(t, summaryValidationCheckpoint("final reduction"), outcome.Text)
	require.Len(t, client.requests, 7)
	assert.Len(t, client.requests[0].Messages, 1)
	assert.Len(t, client.requests[4].Messages, 2)
	assert.Len(t, client.requests[6].Messages, 2)

	for _, request := range client.requests {
		assert.NoError(t, runtime.newSummaryRequest(request).budget.Validate())
	}
}

type recursiveReductionCompleter struct {
	requests []*CompletionRequest
}

func (client *recursiveReductionCompleter) Complete(
	_ context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	client.requests = append(client.requests, request)

	goal := "first reduction"
	padding := 200

	if len(client.requests) > 4 {
		goal = "recursive reduction"
		padding = 140
	}

	if len(client.requests) == 7 {
		goal = "final reduction"
		padding = 0
	}

	return &CompletionResult{
		FinishReason: llm.FinishReasonStop,
		Termination:  llm.NewTerminationMetadata("", "", ""),
		Text:         summaryValidationCheckpoint(goal) + repeatedSummaryTokens(" detail", padding),
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        model.EmptyTokenUsage(),
	}, nil
}

func repeatedSummaryTokens(value string, tokens int) string {
	return strings.Repeat(value+" ", tokens)
}

func TestCompleteSummaryRejectsTruncatedRepair(t *testing.T) {
	t.Parallel()

	client := &summaryValidationCompleter{
		responses:     []string{malformedSummary, summaryValidationCheckpoint("partial repair")},
		finishReasons: []llm.FinishReason{llm.FinishReasonStop, llm.FinishReasonLength},
		calls:         0,
	}
	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) { deps.Client = client })
	request := summaryValidationRequest(summaryValidationBudget())

	_, err := runtime.completeSummary(t.Context(), request, compaction.Operation{
		ID: "repair-truncated", Reason: compaction.ReasonManual, RetryIntent: compaction.RetryNone,
	})

	require.ErrorIs(t, err, compaction.ErrSummaryOutputTruncated)
	assert.Equal(t, 2, client.calls)
}

func summaryValidationBudget() contextwindow.SummaryBudgetInput {
	return contextwindow.SummaryBudgetInput{
		ContextWindow: 16_000, SystemPromptTokens: 20, PreviousSummaryTokens: 0,
		SplitTurnTokens: 0, HistoryTokens: 20, ProviderReserve: 0,
		SafetyMargin: 0, EnvelopeTokens: 10, MaxOutputTokens: 2_000,
	}
}

func summaryValidationCheckpoint(goal string) string {
	return "## Goal\n- " + goal +
		"\n## User constraints and preferences\n- None" +
		"\n## Completed work\n- None\n## Work in progress\n- None" +
		"\n## Files changed/read\n- None\n## Commands and validation\n- None" +
		"\n## Decisions\n- None\n## Errors and blockers\n- None" +
		"\n## Exact next steps\n- None"
}

func summaryValidationRequest(input contextwindow.SummaryBudgetInput) *summaryRequest {
	completion := newZeroCompletionRequest(model.RequestAuth{Headers: nil, APIKey: "", Error: "", OK: false})
	completion.Model.Provider = summaryValidationProvider
	completion.Model.ID = summaryValidationModelID
	completion.MaxTokens = input.MaxOutputTokens

	return &summaryRequest{
		completion: completion,
		budget:     contextwindow.NewSummaryBudget(input),
	}
}
