package assistant

import (
	"context"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

type extensionToolRunner interface {
	ExecuteTool(ctx context.Context, name string, args tool.Arguments) (extension.ToolResult, error)
}

type extensionToolExecutor struct {
	runner     extensionToolRunner
	definition tool.Definition
}

func newExtensionToolExecutor(definition extension.Tool, runner extensionToolRunner) *extensionToolExecutor {
	return &extensionToolExecutor{
		runner: runner,
		definition: tool.Definition{
			Schema:           definition.InputSchema,
			Name:             tool.Name(definition.Name),
			Label:            definition.Name,
			Description:      definition.Description,
			PromptSnippet:    "",
			PromptGuidelines: []string{},
			ReadOnly:         false,
		},
	}
}

func (executor *extensionToolExecutor) Definition() tool.Definition {
	return executor.definition
}

func (executor *extensionToolExecutor) Execute(ctx context.Context, input tool.Arguments) (tool.Result, error) {
	result, err := executor.runner.ExecuteTool(ctx, string(executor.definition.Name), input)
	if err != nil {
		return tool.Result{Details: map[string]any{}, Content: []tool.ContentBlock{}}, oops.
			In("tool").
			Code("execute_extension_tool").
			With("tool", executor.definition.Name).
			Wrapf(err, "execute extension tool")
	}

	return tool.TextResult(result.Content, result.Details), nil
}

func registerExtensionTools(registry *tool.Registry, runner extensionToolRunner, definitions []extension.Tool) error {
	for _, definition := range definitions {
		if err := registry.Register(newExtensionToolExecutor(definition, runner)); err != nil {
			return oops.In("assistant").Code("register_extension_tool").Wrapf(err, "register extension tool")
		}
	}

	return nil
}
