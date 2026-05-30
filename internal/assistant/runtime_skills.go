// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"log/slog"

	"github.com/omarluq/librecode/internal/core"
)

const maxActiveSkillReadLines = 2000

func (runtime *Runtime) emitActivatedSkillReads(
	ctx context.Context,
	cwd string,
	skills []core.ActivatedSkill,
	onEvent func(StreamEvent),
) []ToolEvent {
	if len(skills) == 0 {
		return nil
	}
	limit := maxActiveSkillReadLines
	toolEvents := make([]ToolEvent, 0, len(skills))
	for index := range skills {
		skill := &skills[index].Skill
		_, toolEvent, err := runtime.loadSkillWithReadTool(ctx, cwd, skill, &limit)
		if err != nil {
			runtime.logger.Debug(
				"failed to emit activated skill read",
				slog.String("skill", skill.Name),
				slog.Any("error", err),
			)
		}
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: &toolEvent,
			Usage:     nil,
			Kind:      StreamEventSkillLoaded,
			Text:      skill.Name,
		})
		toolEvents = append(toolEvents, toolEvent)
	}

	return toolEvents
}

func activeSkillEventPayload(skills []core.ActivatedSkill) []map[string]any {
	payload := make([]map[string]any, 0, len(skills))
	for index := range skills {
		skill := skills[index].Skill
		payload = append(payload, map[string]any{
			"name":        skill.Name,
			"description": skill.Description,
			jsonPathKey:   skill.FilePath,
			"truncated":   skills[index].Truncated,
		})
	}

	return payload
}

func activeSkillMatchPayload(matches []core.SkillActivationDiagnostic) []map[string]any {
	payload := make([]map[string]any, 0, len(matches))
	for index := range matches {
		match := matches[index]
		payload = append(payload, map[string]any{
			"name":      match.Skill.Name,
			jsonPathKey: match.Skill.FilePath,
			"reason":    match.Reason,
			"score":     match.Score,
		})
	}

	return payload
}
