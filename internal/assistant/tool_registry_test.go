package assistant_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

func TestRuntimeRegistersExtensionToolsInModelRegistry(t *testing.T) {
	t.Parallel()

	client := newExtensionToolCompleter()
	runtime, _, manager := newTestRuntimeWithManager(t, client)
	loadRuntimeExtension(t, manager, echoToolExtensionSource())

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "use echo", ""))
	require.NoError(t, err)

	assert.Contains(t, toolDefinitionNames(*client.definitions), "echo")
	assert.Equal(t, "hello", *client.toolResult)
	assert.Equal(t, true, (*client.toolDetails)["seen"])
}

func TestRuntimeRejectsExtensionToolNameCollision(t *testing.T) {
	t.Parallel()

	runtime, _, manager := newTestRuntimeWithManager(t, testCompleter{})
	loadRuntimeExtension(t, manager, `
local lc = require("librecode")
lc.register_tool("read", "collides", function()
  return { content = "bad" }
end)
`)

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "hello", ""))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate tool")
}

func TestExtensionToolSchemaIsVisibleInRegistryDefinitions(t *testing.T) {
	t.Parallel()

	client := newExtensionToolCompleter()
	runtime, _, manager := newTestRuntimeWithManager(t, client)
	loadRuntimeExtension(t, manager, echoToolExtensionSource())

	_, err := runtime.Prompt(context.Background(), newRuntimePromptRequest(testRuntimeCWD, "schema", ""))
	require.NoError(t, err)

	definition := findToolDefinition(t, *client.definitions, "echo")
	schema := definition.Schema.MustToMap()
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, properties, "text")
}

type extensionToolCompleter struct {
	toolDetails *map[string]any
	definitions *[]tool.Definition
	toolResult  *string
}

func newExtensionToolCompleter() *extensionToolCompleter {
	definitions := []tool.Definition{}
	toolDetails := map[string]any{}
	toolResult := ""

	return &extensionToolCompleter{
		toolDetails: &toolDetails,
		definitions: &definitions,
		toolResult:  &toolResult,
	}
}

func (client *extensionToolCompleter) Complete(
	ctx context.Context,
	request *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	if request.ToolRegistry == nil {
		return nil, errors.New("missing tool registry")
	}

	*client.definitions = request.ToolRegistry.Definitions()

	result, err := request.ToolRegistry.Execute(ctx, "echo", map[string]any{"text": "hello"})
	if err != nil {
		return nil, fmt.Errorf("execute echo tool: %w", err)
	}

	*client.toolResult = result.Text()
	*client.toolDetails = result.Details

	return &assistant.CompletionResult{
		FinishReason: llm.FinishReasonStop,
		Text:         "tool registry inspected",
		Thinking:     nil,
		ToolEvents:   nil,
		Usage:        model.EmptyTokenUsage(),
	}, nil
}

func echoToolExtensionSource() string {
	return `
local lc = require("librecode")
lc.register_tool("echo", "Echo text", function(args)
  return { content = args.text, details = { seen = true } }
end, {
  type = "object",
  properties = {
    text = { type = "string", description = "Text to echo" },
  },
  required = { "text" },
})
`
}

func toolDefinitionNames(definitions []tool.Definition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, string(definition.Name))
	}

	return names
}

func findToolDefinition(t *testing.T, definitions []tool.Definition, name string) tool.Definition {
	t.Helper()

	for _, definition := range definitions {
		if definition.Name == tool.Name(name) {
			return definition
		}
	}

	require.Failf(t, "tool definition not found", "name=%s", name)

	return tool.Definition{
		Schema:           tool.EmptySchema(),
		Name:             "",
		Label:            "",
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         false,
	}
}
