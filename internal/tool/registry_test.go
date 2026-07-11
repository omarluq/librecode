package tool_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

const (
	registryTestContentKey = "content"
	registryTestEmptyPath  = "empty.txt"
	registryTestPathKey    = "path"
)

func TestNewRegistryWithTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantError error
		name      string
		tools     []tool.Name
		wantNames []tool.Name
	}{
		{
			wantError: nil, name: "allowlist",
			tools: []tool.Name{tool.NameRead, tool.NameGrep}, wantNames: []tool.Name{tool.NameGrep, tool.NameRead},
		},
		{
			wantError: nil, name: "deduplicates",
			tools: []tool.Name{tool.NameRead, tool.NameRead}, wantNames: []tool.Name{tool.NameRead},
		},
		{wantError: tool.ErrUnknownTool, name: "unknown", tools: []tool.Name{"missing"}, wantNames: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			registry, err := tool.NewRegistryWithTools(t.TempDir(), testCase.tools)
			if testCase.wantError != nil {
				require.ErrorIs(t, err, testCase.wantError)
				assert.Nil(t, registry)

				return
			}

			require.NoError(t, err)

			definitions := registry.Definitions()

			names := make([]tool.Name, len(definitions))
			for index := range definitions {
				names[index] = definitions[index].Name
			}

			assert.Equal(t, testCase.wantNames, names)
		})
	}
}

func TestRegistry_ExecuteJSONRunsBuiltInFileTools(t *testing.T) {
	t.Parallel()

	const testPath = "src/main.go"

	ctx := context.Background()
	registry := tool.NewRegistry(t.TempDir())

	writeResult := executeTool(ctx, t, registry, "write", map[string]any{
		registryTestPathKey:    testPath,
		registryTestContentKey: "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n",
	})
	assert.Contains(t, writeResult.Text(), "Successfully wrote")

	findResult := executeTool(ctx, t, registry, "find", map[string]any{"pattern": "**/*.go"})
	assert.Contains(t, findResult.Text(), "src/main.go")

	grepResult := executeTool(ctx, t, registry, "grep", map[string]any{
		"pattern":           "println",
		registryTestPathKey: ".",
		"glob":              "**/*.go",
	})
	assert.Contains(t, grepResult.Text(), "src/main.go:4:")
	assert.Contains(t, grepResult.Text(), "println")

	editResult := executeTool(ctx, t, registry, "edit", map[string]any{
		registryTestPathKey: testPath,
		"edits": []map[string]any{
			{"old_text": "hello", "new_text": "hola"},
		},
	})
	assert.Contains(t, editResult.Text(), "Successfully replaced")
	assert.Contains(t, editResult.Details["diff"], "hola")

	readResult := executeTool(ctx, t, registry, "read", map[string]any{registryTestPathKey: testPath})
	assert.Contains(t, readResult.Text(), "hola")

	lsResult := executeTool(ctx, t, registry, "ls", map[string]any{registryTestPathKey: "src"})
	assert.Equal(t, "main.go", lsResult.Text())
}

func TestRegistry_ExecuteJSONRunsBashInWorkingDirectory(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	result := executeTool(context.Background(), t, registry, "bash", map[string]any{
		"command": "pwd && printf ok",
	})

	assert.Contains(t, result.Text(), "ok")
}

func TestRegistry_Register(t *testing.T) {
	t.Parallel()

	t.Run("rejects duplicate tool", func(t *testing.T) {
		t.Parallel()

		registry := tool.NewRegistry(t.TempDir())

		err := registry.Register(tool.NewReadTool(t.TempDir()))

		require.Error(t, err)
		assert.ErrorIs(t, err, tool.ErrDuplicateTool)
	})

	t.Run("adds new tool", func(t *testing.T) {
		t.Parallel()

		registry := tool.NewRegistry(t.TempDir())
		custom := registryTestExecutor{name: "custom"}

		require.NoError(t, registry.Register(custom))
		result, err := registry.Execute(context.Background(), "custom", tool.EmptyArguments())

		require.NoError(t, err)
		assert.Equal(t, "custom ok", result.Text())
	})
}

func TestRegistry_Metadata(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	registry := tool.NewRegistry(cwd)

	assert.Equal(t, cwd, registry.CWD())
	assert.NotEmpty(t, registry.Definitions())
	assert.Len(t, tool.AllDefinitions(), len(registry.Definitions()))
}

func TestRegistry_ExecuteJSONValidatesInputsBeforeExecution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input       map[string]any
		name        string
		tool        string
		wantErrText string
	}{
		{name: "bash command", tool: "bash", input: map[string]any{}, wantErrText: "command is required"},
		{
			name:        "write content",
			tool:        "write",
			input:       map[string]any{registryTestPathKey: registryTestEmptyPath},
			wantErrText: registryTestContentKey + " is required",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			payload, err := json.Marshal(testCase.input)
			require.NoError(t, err)

			_, err = tool.NewRegistry(t.TempDir()).ExecuteJSON(context.Background(), testCase.tool, payload)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErrText)
		})
	}
}

func TestRegistry_ExecuteJSONAllowsIntentionalEmptyWriteContent(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	result := executeTool(context.Background(), t, registry, "write", map[string]any{
		registryTestPathKey:    registryTestEmptyPath,
		registryTestContentKey: "",
	})

	assert.Contains(t, result.Text(), "Successfully wrote 0B")
}

func TestRegistry_ExecuteValidatesEmptyInput(t *testing.T) {
	t.Parallel()

	_, err := tool.NewRegistry(t.TempDir()).Execute(context.Background(), "bash", tool.EmptyArguments())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bash command is required")
}

type registryTestExecutor struct {
	name tool.Name
}

func (executor registryTestExecutor) Definition() tool.Definition {
	return tool.Definition{
		Schema:           tool.EmptySchema(),
		Name:             executor.name,
		Label:            string(executor.name),
		Description:      "test tool",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         true,
	}
}

func (executor registryTestExecutor) Execute(context.Context, tool.Arguments) (tool.Result, error) {
	return tool.TextResult(string(executor.name)+" ok", nil), nil
}

func executeTool(
	ctx context.Context,
	t *testing.T,
	registry *tool.Registry,
	name string,
	input map[string]any,
) tool.Result {
	t.Helper()

	payload, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := registry.ExecuteJSON(ctx, name, payload)
	require.NoError(t, err)

	return result
}

func TestRegistry_ExecuteErrors(t *testing.T) {
	t.Parallel()

	tests := []struct { //nolint:govet // Table-driven tests prefer readable field order over fieldalignment.
		name        string
		toolName    string
		payload     []byte
		wantErrText string
	}{
		{name: "unknown tool", toolName: "missing", payload: []byte(`{}`), wantErrText: "unknown tool"},
		{name: "invalid json", toolName: "read", payload: []byte(`{`), wantErrText: "decode tool arguments"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			registry := tool.NewRegistry(t.TempDir())
			_, err := registry.ExecuteJSON(context.Background(), testCase.toolName, testCase.payload)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErrText)
		})
	}
}
