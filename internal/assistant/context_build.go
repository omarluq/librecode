package assistant

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/model"
)

const (
	contextContributionSourceExtension = "extension"
	contextContributionRoleSystem      = "system"
	contextContributionMaxTokens       = 2048
)

type contextContribution struct {
	Metadata map[string]any
	Source   string
	Name     string
	Role     string
	Content  string
	Tokens   int
}

type contextBuildResult struct {
	Breakdown     map[string]int
	SystemPrompt  string
	Contributions []contextContribution
	Messages      []database.MessageEntity
	Usage         model.TokenUsage
}

type modelContextBase struct {
	BaseSystemPrompt string
	SkillPrompt      string
	SystemPrompt     string
	ActiveSkills     []core.ActivatedSkill
	SkillDiagnostics []core.SkillActivationDiagnostic
	Messages         []database.MessageEntity
	SystemTokens     int
	SkillTokens      int
	HistoryTokens    int
}

func (runtime *Runtime) buildModelContext(
	ctx context.Context,
	sessionID string,
	cwd string,
	prompt string,
	selectedModel *model.Model,
	onEvent func(StreamEvent),
) (*contextBuildResult, error) {
	messages, err := runtime.modelContextMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	base, err := runtime.modelContextBase(ctx, cwd, prompt, messages, onEvent)
	if err != nil {
		return nil, err
	}
	result := initialContextBuildResult(&base, selectedModel)

	dispatchResult, err := runtime.dispatchContextBuild(ctx, sessionID, cwd, &base, result)
	if err != nil {
		return nil, err
	}

	contributions, err := contextContributionsFromPayload(dispatchResult.Payload)
	if err != nil {
		return nil, err
	}
	appendContextContributions(result, contributions)
	recalculateContextBuildResult(result, &base, selectedModel)
	runtime.emit(
		ctx,
		string(extension.LifecycleContextBuild),
		contextBuildLifecyclePayload(sessionID, cwd, &base, result),
	)

	return result, nil
}

func (runtime *Runtime) modelContextBase(
	ctx context.Context,
	cwd string,
	prompt string,
	messages []database.MessageEntity,
	onEvent func(StreamEvent),
) (modelContextBase, error) {
	basePrompt := baseSystemPrompt(cwd)
	skills := core.LoadSkills(cwd, nil, true).Skills
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
		runtime.emit(ctx, "skill_auto_activate", payload)
		if runtime.extensions != nil {
			if emitErr := runtime.extensions.Emit(ctx, "skill_auto_activate", payload); emitErr != nil {
				return modelContextBase{}, oops.In("assistant").
					Code("skill_auto_activate").
					Wrapf(emitErr, "emit skill auto activation")
			}
		}
	}

	return modelContextBase{
		ActiveSkills:     activeSkills,
		SkillDiagnostics: skillActivation.Matches,
		Messages:         messages,
		SystemPrompt:     basePrompt + availableSkillsPrompt + activeSkillsPrompt,
		BaseSystemPrompt: basePrompt,
		SkillPrompt:      availableSkillsPrompt + activeSkillsPrompt,
		SystemTokens:     estimateTokens(basePrompt),
		SkillTokens:      estimateTokens(availableSkillsPrompt) + estimateTokens(activeSkillsPrompt),
		HistoryTokens:    estimateMessageTokens(messages),
	}, nil
}

func initialContextBuildResult(base *modelContextBase, selectedModel *model.Model) *contextBuildResult {
	breakdown := contextBreakdown(base.SystemTokens, base.SkillTokens, base.HistoryTokens, nil)

	return &contextBuildResult{
		Contributions: []contextContribution{},
		Messages:      base.Messages,
		Breakdown:     breakdown,
		SystemPrompt:  base.SystemPrompt,
		Usage: estimateContextBuildUsage(
			base.SystemPrompt,
			base.Messages,
			nil,
			selectedModel,
			breakdown,
		),
	}
}

func recalculateContextBuildResult(
	result *contextBuildResult,
	base *modelContextBase,
	selectedModel *model.Model,
) {
	result.Breakdown = contextBreakdown(
		base.SystemTokens,
		base.SkillTokens,
		base.HistoryTokens,
		result.Contributions,
	)
	result.Usage = estimateContextBuildUsage(
		result.SystemPrompt,
		base.Messages,
		result.Contributions,
		selectedModel,
		result.Breakdown,
	)
}

func estimateContextBuildUsage(
	systemPrompt string,
	messages []database.MessageEntity,
	contributions []contextContribution,
	selectedModel *model.Model,
	breakdown map[string]int,
) model.TokenUsage {
	inputTokens := estimateInputTokens(systemPrompt, messages)
	for index := range contributions {
		inputTokens += contributions[index].Tokens
	}

	return model.TokenUsage{
		Breakdown:     cloneIntMapForUsage(breakdown),
		ContextWindow: selectedModel.ContextWindow,
		ContextTokens: inputTokens,
		InputTokens:   inputTokens,
		OutputTokens:  0,
	}
}

func appendContextContributions(result *contextBuildResult, contributions []contextContribution) {
	if len(contributions) == 0 {
		return
	}

	builder := strings.Builder{}
	builder.WriteString(result.SystemPrompt)
	builder.WriteString("\n\n<extension_context>")
	for index := range contributions {
		contribution := contributions[index]
		result.Contributions = append(result.Contributions, contribution)
		builder.WriteString("\n<block")
		if contribution.Name != "" {
			builder.WriteString(" name=")
			builder.WriteString(strconv.Quote(contribution.Name))
		}
		builder.WriteString(" source=")
		builder.WriteString(strconv.Quote(contribution.Source))
		builder.WriteString(" role=")
		builder.WriteString(strconv.Quote(contribution.Role))
		builder.WriteString(" tokens=")
		builder.WriteString(strconv.Itoa(contribution.Tokens))
		builder.WriteString(">\n")
		builder.WriteString(contribution.Content)
		builder.WriteString("\n</block>")
	}
	builder.WriteString("\n</extension_context>")
	result.SystemPrompt = builder.String()
}

func contextBreakdown(
	systemTokens int,
	skillTokens int,
	historyTokens int,
	contributions []contextContribution,
) map[string]int {
	breakdown := map[string]int{
		jsonSystemRole: systemTokens,
		"skills":       skillTokens,
		"history":      historyTokens,
		"extensions":   0,
	}
	for index := range contributions {
		breakdown["extensions"] += contributions[index].Tokens
	}

	return breakdown
}

func contextContributionsFromPayload(payload map[string]any) ([]contextContribution, error) {
	raw, found := payload["contributions"]
	if !found || raw == nil {
		return []contextContribution{}, nil
	}

	rawContributions, ok := raw.([]any)
	if !ok {
		if rawMap, mapOK := raw.(map[string]any); mapOK {
			rawContributions = numericMapValues(rawMap)
		} else {
			return nil, oops.In("assistant").
				Code("invalid_context_contributions").
				Errorf("context contributions must be a list")
		}
	}

	contributions := make([]contextContribution, 0, len(rawContributions))
	for index, rawContribution := range rawContributions {
		contribution, err := contextContributionFromValue(rawContribution)
		if err != nil {
			return nil, oops.In("assistant").
				Code("invalid_context_contribution").
				Wrapf(err, "context contribution %d", index)
		}
		contributions = append(contributions, contribution)
	}

	return contributions, nil
}

func numericMapValues(values map[string]any) []any {
	items := make([]any, 0, len(values))
	for index := 1; index <= len(values); index++ {
		value, ok := values[fmt.Sprint(index)]
		if !ok {
			return []any{}
		}
		items = append(items, value)
	}

	return items
}

func contextContributionFromValue(value any) (contextContribution, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return contextContribution{}, fmt.Errorf("must be an object")
	}
	content := strings.TrimSpace(stringFromAny(object[jsonContentKey]))
	if content == "" {
		return contextContribution{}, fmt.Errorf("content is required")
	}
	tokens := estimateTokens(content)
	if tokens > contextContributionMaxTokens {
		return contextContribution{}, fmt.Errorf(
			"content exceeds %d-token contribution limit",
			contextContributionMaxTokens,
		)
	}

	source := stringFromAny(object["source"])
	if source == "" {
		source = contextContributionSourceExtension
	}
	role := stringFromAny(object[jsonRoleKey])
	if role == "" {
		role = contextContributionRoleSystem
	}

	return contextContribution{
		Metadata: mapFromAny(object["metadata"]),
		Source:   source,
		Name:     stringFromAny(object[jsonToolNameKey]),
		Role:     role,
		Content:  content,
		Tokens:   tokens,
	}, nil
}

func stringFromAny(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}

	return ""
}

func mapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}

	return map[string]any{}
}
