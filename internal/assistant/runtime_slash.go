// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/tool"
)

const slashPrefix = "/"

func (runtime *Runtime) respondToSlashCommand(
	ctx context.Context,
	cwd string,
	prompt string,
	onEvent func(StreamEvent),
) (string, []ToolEvent, error) {
	commandName, commandArgs := splitSlashCommand(prompt)
	if commandName == "" {
		return "", nil, oops.In("assistant").Code("empty_slash_command").Errorf("empty slash command")
	}

	if commandName == "skill" {
		return runtime.respondToSkillCommand(ctx, cwd, commandArgs, onEvent)
	}
	if commandName == "tool" {
		response, err := runtime.respondToToolCommand(ctx, cwd, commandArgs)
		return response, nil, err
	}

	response, err := runtime.extensions.ExecuteCommand(ctx, commandName, commandArgs)
	if err != nil {
		return "", nil, oops.
			In("assistant").
			Code("extension_command").
			With("command", commandName).
			Wrapf(err, "execute command")
	}

	return response, nil, nil
}

func (runtime *Runtime) respondToSkillCommand(
	ctx context.Context,
	cwd string,
	args string,
	onEvent func(StreamEvent),
) (string, []ToolEvent, error) {
	skills := core.LoadSkills(cwd, nil, true).Skills
	name := strings.TrimSpace(args)
	if name == "" {
		if len(skills) == 0 {
			return "No skills found.", nil, nil
		}
		lines := []string{"Available skills:"}
		for index := range skills {
			lines = append(lines, fmt.Sprintf("- %s: %s", skills[index].Name, skills[index].Description))
		}

		return strings.Join(lines, "\n"), nil, nil
	}

	for index := range skills {
		skill := &skills[index]
		if skill.Name != name {
			continue
		}
		result, toolEvent, err := runtime.loadSkillWithReadTool(ctx, cwd, skill, nil)
		if err != nil {
			return "", nil, err
		}
		emitStreamEvent(onEvent, StreamEvent{
			ToolEvent: &toolEvent,
			Usage:     nil,
			Kind:      StreamEventSkillLoaded,
			Text:      skill.Name,
		})

		return result, []ToolEvent{toolEvent}, nil
	}

	return "", nil, oops.In("assistant").Code("skill_not_found").With("skill", name).Errorf("skill %q not found", name)
}

func (runtime *Runtime) loadSkillWithReadTool(
	ctx context.Context,
	cwd string,
	skill *core.Skill,
	limit *int,
) (string, ToolEvent, error) {
	registry := tool.NewRegistry(cwd)
	input := map[string]any{jsonPathKey: skill.FilePath}
	if limit != nil {
		input["limit"] = *limit
	}
	result, err := registry.Execute(ctx, string(tool.NameRead), input)
	toolEvent := ToolEvent{
		Name:          "load skill: " + skill.Name,
		ArgumentsJSON: skillReadArgumentsJSON(skill.FilePath, limit),
		DetailsJSON:   "",
		Result:        result.Text(),
		Error:         "",
	}
	if err != nil {
		toolEvent.Error = err.Error()
		return "", toolEvent, oops.In("assistant").Code("skill_read").Wrapf(err, "load skill with read tool")
	}

	return result.Text(), toolEvent, nil
}

func skillReadArgumentsJSON(path string, limit *int) string {
	if limit == nil {
		return fmt.Sprintf(`{"path":%q}`, path)
	}

	return fmt.Sprintf(`{"path":%q,"limit":%d}`, path, *limit)
}

func (runtime *Runtime) respondToToolCommand(ctx context.Context, cwd, args string) (string, error) {
	toolName, payload, found := strings.Cut(strings.TrimSpace(args), " ")
	if toolName == "" {
		return "", fmt.Errorf("assistant: tool command requires a tool name")
	}
	if !found || strings.TrimSpace(payload) == "" {
		payload = "{}"
	}

	registry := tool.NewRegistry(cwd)
	result, err := registry.ExecuteJSON(ctx, toolName, []byte(payload))
	if err != nil {
		return "", oops.
			In("assistant").
			Code("builtin_tool").
			With("tool", toolName).
			Wrapf(err, "execute built-in tool")
	}

	return result.Text(), nil
}
