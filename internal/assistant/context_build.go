package assistant

import (
	"context"
	"log/slog"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

// ContextUsage estimates the current model-facing context without executing a prompt.
// It is intended for diagnostics such as /context and intentionally avoids
// prompt-dependent skill activation and extension context mutation.
func (runtime *Runtime) ContextUsage(ctx context.Context, sessionID, cwd string) (model.TokenUsage, error) {
	selectedModel, err := runtime.selectedModel()
	if err != nil {
		return model.EmptyTokenUsage(), err
	}

	registry, err := newToolRegistry(cwd, runtime.extensions)
	if err != nil {
		return model.EmptyTokenUsage(), err
	}

	request := &CompletionRequest{
		OnEvent:           nil,
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ToolRegistry:      registry,
		ExecuteTools:      nil,
		SessionID:         sessionID,
		SystemPrompt:      "",
		ThinkingLevel:     "",
		CWD:               cwd,
		Auth:              model.RequestAuth{Headers: nil, APIKey: "", Error: "", OK: false},
		Messages:          nil,
		Usage:             model.EmptyTokenUsage(),
		Model:             selectedModel,
		ProviderAttempt:   0,
		DisableTools:      false,
	}

	messages := []database.MessageEntity{}

	var usageAnchor *database.ContextUsageAnchorEntity

	if strings.TrimSpace(sessionID) != "" {
		contextEntity, err := runtime.modelContextEntity(ctx, sessionID)
		if err != nil {
			return model.EmptyTokenUsage(), err
		}

		messages = modelFacingMessages(contextEntity.Messages)
		usageAnchor = remapUsageAnchor(contextEntity.UsageAnchor, contextEntity.Messages, messages)
	}

	basePrompt := runtime.baseSystemPrompt(cwd)

	skillPrompt := ""
	if skills := runtime.loadSkills(cwd); len(skills) > 0 {
		skillPrompt = core.FormatSkillsForPrompt(skills)
	}

	breakdown := contextwindow.Breakdown(
		contextwindow.EstimateTokens(basePrompt),
		contextwindow.EstimateTokens(skillPrompt),
		contextwindow.EstimateMessageTokens(messages),
		nil,
	)

	usage := contextwindow.EstimateBuildUsage(
		basePrompt+skillPrompt,
		messages,
		nil,
		&selectedModel,
		breakdown,
		usageAnchor,
	)
	budget := contextwindow.NewBudget(
		usage,
		&selectedModel,
		runtime.cfg.Context,
		func() int { return runtime.estimateToolSchemaTokens(request) },
	)

	return budget.UsageWithBudget(usage), nil
}

func (runtime *Runtime) buildModelContext(
	ctx context.Context,
	sessionID string,
	cwd string,
	prompt string,
	selectedModel *model.Model,
	onEvent func(StreamEvent),
) (*contextwindow.BuildResult, error) {
	contextEntity, err := runtime.modelContextEntity(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	messages := modelFacingMessages(contextEntity.Messages)
	usageAnchor := remapUsageAnchor(contextEntity.UsageAnchor, contextEntity.Messages, messages)

	base, err := runtime.modelContextBase(ctx, cwd, prompt, messages, usageAnchor, onEvent)
	if err != nil {
		return nil, err
	}

	result := initialContextBuildResult(&base, selectedModel)

	if runtime.childDefinition != nil {
		return result, nil
	}

	dispatchResult, err := runtime.dispatchContextBuild(ctx, sessionID, cwd, &base, result)
	if err != nil {
		return nil, err
	}

	contributions, err := contextwindow.ContributionsFromPayload(dispatchResult.Payload)
	if err != nil {
		return nil, assistantError(err, "parse context contributions")
	}

	contextwindow.AppendContributions(result, contributions)
	recalculateContextBuildResult(result, &base, selectedModel)

	return result, nil
}

func (runtime *Runtime) modelContextBase(
	ctx context.Context,
	cwd string,
	prompt string,
	messages []database.MessageEntity,
	usageAnchor *database.ContextUsageAnchorEntity,
	onEvent func(StreamEvent),
) (contextwindow.Base, error) {
	basePrompt := runtime.baseSystemPrompt(cwd)

	skills := runtime.loadSkills(cwd)
	if runtime.childDefinition != nil {
		skills = nil
	}

	availableSkillsPrompt := ""
	if len(skills) > 0 {
		availableSkillsPrompt = core.FormatSkillsForPrompt(skills)
	}

	skillActivation := core.AutoActivateSkillsDetailed(prompt, skills)

	activeSkills := skillActivation.Activated
	if len(skillActivation.Diagnostics) > 0 && runtime.logger != nil {
		runtime.logger.Debug("skill auto-activation diagnostics", slog.Int("count", len(skillActivation.Diagnostics)))
	}

	for index := range skillActivation.Matches {
		match := &skillActivation.Matches[index]
		if runtime.logger != nil {
			runtime.logger.Debug(
				"skill auto-activated",
				slog.String("skill", match.Skill.Name),
				slog.String("reason", match.Reason),
				slog.Int("score", match.Score),
			)
		}
	}

	activeSkillsPrompt := ""

	runtime.emitActivatedSkillReads(ctx, cwd, activeSkills, onEvent)

	if len(activeSkills) > 0 {
		activeSkillsPrompt = core.FormatActiveSkillsForPrompt(activeSkills)
		payload := map[string]any{
			"skills":  activeSkillEventPayload(activeSkills),
			"matches": activeSkillMatchPayload(skillActivation.Matches),
		}

		if runtime.extensions != nil {
			if emitErr := runtime.extensions.Emit(ctx, "skill_auto_activate", payload); emitErr != nil {
				return contextwindow.Base{}, oops.In("assistant").
					Code("skill_auto_activate").
					Wrapf(emitErr, "emit skill auto activation")
			}
		}
	}

	return contextwindow.Base{
		ActiveSkills:     activeSkills,
		SkillDiagnostics: skillActivation.Matches,
		Messages:         messages,
		UsageAnchor:      usageAnchor,
		SystemPrompt:     basePrompt + availableSkillsPrompt + activeSkillsPrompt,
		BaseSystemPrompt: basePrompt,
		SkillPrompt:      availableSkillsPrompt + activeSkillsPrompt,
		SystemTokens:     contextwindow.EstimateTokens(basePrompt),
		SkillTokens: contextwindow.EstimateTokens(availableSkillsPrompt) +
			contextwindow.EstimateTokens(activeSkillsPrompt),
		HistoryTokens: contextwindow.EstimateMessageTokens(messages),
	}, nil
}

func initialContextBuildResult(base *contextwindow.Base, selectedModel *model.Model) *contextwindow.BuildResult {
	breakdown := contextwindow.Breakdown(base.SystemTokens, base.SkillTokens, base.HistoryTokens, nil)

	return &contextwindow.BuildResult{
		Contributions: []contextwindow.Contribution{},
		Messages:      base.Messages,
		Breakdown:     breakdown,
		SystemPrompt:  base.SystemPrompt,
		UsageAnchor:   base.UsageAnchor,
		Usage: contextwindow.EstimateBuildUsage(
			base.SystemPrompt,
			base.Messages,
			nil,
			selectedModel,
			breakdown,
			base.UsageAnchor,
		),
	}
}

func recalculateContextBuildResult(
	result *contextwindow.BuildResult,
	base *contextwindow.Base,
	selectedModel *model.Model,
) {
	result.Breakdown = contextwindow.Breakdown(
		base.SystemTokens,
		base.SkillTokens,
		base.HistoryTokens,
		result.Contributions,
	)
	result.UsageAnchor = base.UsageAnchor
	result.Usage = contextwindow.EstimateBuildUsage(
		result.SystemPrompt,
		base.Messages,
		result.Contributions,
		selectedModel,
		result.Breakdown,
		base.UsageAnchor,
	)
}
