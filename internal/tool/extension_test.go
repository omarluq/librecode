package tool_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

type extensionRunnerFunc func(context.Context, string, map[string]any) (extension.ToolResult, error)

type extensionExecutorCase struct {
	runner         extensionRunnerFunc
	expectedDetail map[string]any
	name           string
	expectedText   string
	expectedErr    string
	expectedCode   string
}

func (runner extensionRunnerFunc) ExecuteTool(
	ctx context.Context,
	name string,
	args map[string]any,
) (extension.ToolResult, error) {
	return runner(ctx, name, args)
}

func TestExtensionExecutor_Execute(t *testing.T) {
	t.Parallel()

	sentinelErr := errors.New("boom")
	tests := []extensionExecutorCase{
		successfulExtensionExecutorCase(),
		failingExtensionExecutorCase(sentinelErr),
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := tool.NewExtensionExecutor(extension.Tool{
				InputSchema: map[string]any{},
				Name:        "echo",
				Description: "Echo text",
				Extension:   "test",
			}, testCase.runner)

			result, err := executor.Execute(context.Background(), map[string]any{"text": "hello"})

			if testCase.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.expectedErr)
				oopsErr, ok := oops.AsOops(err)
				require.True(t, ok)
				assert.Equal(t, testCase.expectedCode, oopsErr.Code())
				assert.Empty(t, result.Text())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedText, result.Text())
			assert.Equal(t, testCase.expectedDetail, result.Details)
			assert.Equal(t, tool.Name("echo"), executor.Definition().Name)
		})
	}
}

func successfulExtensionExecutorCase() extensionExecutorCase {
	return extensionExecutorCase{
		runner: func(context.Context, string, map[string]any) (extension.ToolResult, error) {
			return extension.ToolResult{
				Details: map[string]any{"ok": true},
				Content: "done",
			}, nil
		},
		expectedDetail: map[string]any{"ok": true},
		name:           "success",
		expectedText:   "done",
		expectedErr:    "",
		expectedCode:   "",
	}
}

func failingExtensionExecutorCase(err error) extensionExecutorCase {
	return extensionExecutorCase{
		runner: func(context.Context, string, map[string]any) (extension.ToolResult, error) {
			return extension.ToolResult{Details: nil, Content: ""}, err
		},
		expectedDetail: nil,
		name:           "wraps error",
		expectedText:   "",
		expectedErr:    "execute extension tool",
		expectedCode:   "execute_extension_tool",
	}
}
