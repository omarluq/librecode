package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
