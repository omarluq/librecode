package assistant

import (
	"context"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

// validateCompactionResult proves that the exact persisted summary plus retained
// tail fits the next ordinary request budget before the compaction is committed.
func (runtime *Runtime) validateCompactionResult(
	ctx context.Context,
	cwd, summary string,
	branch []database.EntryEntity,
	plan *compaction.Plan,
	providerInput *compactionProviderInput,
) error {
	messages := []database.MessageEntity{{
		Timestamp: time.Time{}, Role: database.RoleCompactionSummary, Content: summary,
		Provider: "", Model: "", Parts: nil,
	}}

	for index := plan.FirstKeptEntryIndex; index < len(branch); index++ {
		entry := &branch[index]
		if !entry.ModelFacing || !model.IsFacingMessage(&entry.Message) {
			continue
		}

		messages = append(messages, model.FacingMessage(&entry.Message))
	}

	systemPrompt := runtime.baseSystemPrompt(cwd)
	if skills := runtime.loadSkills(cwd); len(skills) > 0 {
		systemPrompt += core.FormatSkillsForPrompt(skills)
	}

	budgetModel := *providerInput.selectedModel
	if budgetModel.ContextWindow < contextwindow.MinimumSummaryOutputTokens {
		budgetModel.ContextWindow = defaultUnknownSummaryContextWindow
	}

	usage := contextwindow.EstimateBuildUsage(systemPrompt, messages, nil, &budgetModel,
		contextwindow.Breakdown(contextwindow.EstimateTokens(systemPrompt), 0,
			contextwindow.EstimateMessageTokens(messages), nil), nil)

	registry, err := newToolRegistry(cwd, runtime.extensions)
	if err != nil {
		return err
	}

	request := &CompletionRequest{
		OnEvent: nil, OnProviderObserve: nil, OnProviderResponse: nil, OnProviderRequest: nil,
		OnRoundCheckpoint: nil, ToolRegistry: registry, ExecuteTools: nil, CWD: cwd, SystemPrompt: systemPrompt,
		ThinkingLevel: "", SessionID: "", Identity: emptyRequestIdentity(),
		Auth: model.RequestAuth{Headers: nil, APIKey: "", Error: "", OK: false}, Messages: messages,
		Usage: usage, Model: budgetModel, ProviderAttempt: 0, MaxTokens: 0, DisableTools: false,
		ToolSideEffectsStarted: false,
	}

	budget := contextwindow.NewBudget(&usage, &budgetModel, &runtime.cfg.Context,
		func() int { return runtime.estimateToolSchemaTokens(request) })
	if err := budget.Validate(); err != nil {
		return &compaction.SummaryError{Kind: compaction.ErrSummaryInputOverflow, Cause: err,
			Provider: providerInput.selectedModel.Provider, Model: providerInput.selectedModel.ID,
			Reason: providerInput.operation.Reason, Input: usage.ContextTokens, Limit: budget.UsableInput,
			Before: plan.TokensBefore, After: usage.ContextTokens}
	}

	if err := ctx.Err(); err != nil {
		return oops.In("assistant").Code("compact_validation_context").Wrapf(err, "validate compacted context")
	}

	return nil
}
