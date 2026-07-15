// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/tool"
)

const slashPrefix = "/"

func promptModelFacing(prompt string) bool {
	return !strings.HasPrefix(strings.TrimSpace(prompt), slashPrefix)
}

func (runtime *Runtime) respondToSlashCommand(
	ctx context.Context,
	cwd string,
	prompt string,
	onEvent func(StreamEvent),
) (response string, toolEvents []ToolEvent, err error) {
	commandName, commandArgs := splitSlashCommand(prompt)
	if commandName == "" {
		return "", nil, oops.In("assistant").Code("empty_slash_command").Errorf("empty slash command")
	}

	if commandName == "skill" {
		return runtime.respondToSkillCommand(ctx, cwd, commandArgs, onEvent)
	}

	if commandName == "tool" {
		response, err = runtime.respondToToolCommand(ctx, cwd, commandArgs)

		return response, nil, err
	}

	response, err = runtime.extensions.ExecuteCommand(ctx, commandName, commandArgs)
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
) (response string, toolEvents []ToolEvent, err error) {
	skills := runtime.loadSkills(cwd)

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
			ToolCallEvent: nil,
			ToolEvent:     &toolEvent,
			Usage:         nil,
			Kind:          StreamEventSkillLoaded,
			Text:          skill.Name,
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
) (content string, toolEvent ToolEvent, err error) {
	registry := tool.NewRegistry(cwd)

	payload, err := skillReadArgumentsJSON(skill.FilePath, limit)
	if err != nil {
		return "", ToolEvent{
			CallID:        "",
			ParentCallID:  "",
			Name:          "",
			ArgumentsJSON: "",
			DetailsJSON:   "",
			Result:        "",
			Error:         "",
			Sequence:      0,
			IsError:       false,
		}, assistantError(err, "build skill read arguments")
	}

	arguments, err := tool.ArgumentsFromRaw(payload)
	if err != nil {
		return "", ToolEvent{
			CallID:        "",
			ParentCallID:  "",
			Name:          "",
			ArgumentsJSON: "",
			DetailsJSON:   "",
			Result:        "",
			Error:         "",
			Sequence:      0,
			IsError:       false,
		}, assistantError(err, "build skill read arguments")
	}

	result, err := registry.Execute(ctx, string(tool.NameRead), arguments)

	toolEvent = ToolEvent{
		CallID:        "",
		ParentCallID:  "",
		Name:          "load skill: " + skill.Name,
		ArgumentsJSON: string(payload),
		DetailsJSON:   "",
		Result:        "",
		Error:         "",
		Sequence:      0,
		IsError:       false,
	}
	if err != nil {
		toolEvent.Error = err.Error()
		toolEvent.IsError = true

		return "", toolEvent, oops.In("assistant").Code("skill_read").Wrapf(err, "load skill with read tool")
	}

	toolEvent.Result = result.Text()

	return toolEvent.Result, toolEvent, nil
}

func skillReadArgumentsJSON(path string, limit *int) ([]byte, error) {
	input := skillReadInput{Limit: limit, Path: path}

	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, assistantError(err, "encode skill read arguments")
	}

	return encoded, nil
}

type skillReadInput struct {
	Limit *int   `json:"limit,omitempty"`
	Path  string `json:"path"`
}

func (runtime *Runtime) respondToToolCommand(ctx context.Context, cwd, args string) (string, error) {
	toolName, payload, found := strings.Cut(strings.TrimSpace(args), " ")
	if toolName == "" {
		return "", oops.In("assistant").Code("tool_command_missing_name").Errorf("tool command requires a tool name")
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
