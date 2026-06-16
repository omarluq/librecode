package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/di"
	builtintool "github.com/omarluq/librecode/internal/tool"
)

func TestToolPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stdin    string
		expected string
		args     []string
	}{
		{name: "default", args: nil, stdin: "", expected: `{}`},
		{name: "blank stdin", args: []string{"-"}, stdin: "  \n", expected: `{}`},
		{name: "stdin JSON", args: []string{"-"}, stdin: `{"command":"pwd"}`, expected: `{"command":"pwd"}`},
		{name: "joined args", args: []string{`{"command":`, `"pwd"}`}, stdin: "", expected: `{"command": "pwd"}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			cmd.SetIn(strings.NewReader(testCase.stdin))

			payload, err := toolPayload(cmd, testCase.args)
			require.NoError(t, err)
			assert.JSONEq(t, testCase.expected, string(payload))
		})
	}
}

func TestToolPayloadRejectsStdinAboveLimit(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(strings.Repeat("a", int(toolJSONStdinLimitBytes)+1)))

	payload, err := toolPayload(cmd, []string{"-"})
	require.Error(t, err)
	assert.Empty(t, payload)
	assert.Contains(t, err.Error(), "tool JSON stdin exceeds limit")
}

func TestPrintToolDefinition(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	output := new(bytes.Buffer)
	cmd.SetOut(output)

	definition := toolRegistryForTest(t).Definitions()[0]

	require.NoError(t, printToolDefinition(cmd, &definition))

	assert.Contains(t, output.String(), definition.Name)
	assert.Contains(t, output.String(), definition.Description)
}

func TestToolRegistryForCWD(t *testing.T) {
	t.Parallel()

	service := &di.ToolService{Registry: toolRegistryForTest(t)}

	defaultRegistry, err := toolRegistryForCWD(service, "")
	require.NoError(t, err)
	assert.Same(t, service.Registry, defaultRegistry)

	cwd := t.TempDir()
	registry, err := toolRegistryForCWD(service, filepath.Join(cwd, "."))
	require.NoError(t, err)
	require.NotNil(t, registry)
	assert.NotSame(t, service.Registry, registry)
}

func toolRegistryForTest(t *testing.T) *builtintool.Registry {
	t.Helper()

	return builtintool.NewRegistry(t.TempDir())
}
