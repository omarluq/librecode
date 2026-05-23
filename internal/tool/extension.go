package tool

import (
	"context"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/extension"
)

// ExtensionToolRunner runs one named extension-provided tool.
type ExtensionToolRunner interface {
	ExecuteTool(ctx context.Context, name string, args map[string]any) (extension.ToolResult, error)
}

// ExtensionExecutor adapts an extension-provided tool to the core tool executor interface.
type ExtensionExecutor struct {
	runner     ExtensionToolRunner
	definition Definition
}

// NewExtensionExecutor creates a core tool executor for an extension-provided tool.
func NewExtensionExecutor(definition extension.Tool, runner ExtensionToolRunner) *ExtensionExecutor {
	return &ExtensionExecutor{
		runner: runner,
		definition: Definition{
			Schema:           definition.InputSchema,
			Name:             Name(definition.Name),
			Label:            definition.Name,
			Description:      definition.Description,
			PromptSnippet:    "",
			PromptGuidelines: []string{},
			ReadOnly:         false,
		},
	}
}

// Definition returns model-visible extension tool metadata.
func (executor *ExtensionExecutor) Definition() Definition {
	return executor.definition
}

// Execute runs the extension tool and converts its result into the core tool result shape.
func (executor *ExtensionExecutor) Execute(ctx context.Context, input map[string]any) (Result, error) {
	result, err := executor.runner.ExecuteTool(ctx, string(executor.definition.Name), input)
	if err != nil {
		return emptyToolResult(), oops.
			In("tool").
			Code("execute_extension_tool").
			With("tool", executor.definition.Name).
			Wrapf(err, "execute extension tool")
	}

	return TextResult(result.Content, result.Details), nil
}

// RegisterExtensions registers extension-provided tools on a core registry.
func (registry *Registry) RegisterExtensions(runner ExtensionToolRunner, definitions []extension.Tool) error {
	for _, definition := range definitions {
		if err := registry.Register(NewExtensionExecutor(definition, runner)); err != nil {
			return err
		}
	}

	return nil
}
