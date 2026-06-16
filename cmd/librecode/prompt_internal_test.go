package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePromptRunOptions(t *testing.T) {
	t.Parallel()

	const testSessionID = "session-1"

	tests := []struct {
		wantErr string
		name    string
		options promptRunOptions
	}{
		{
			options: promptRunOptions{SessionID: "", SessionName: "", Resume: false},
			name:    "default",
			wantErr: "",
		},
		{
			options: promptRunOptions{SessionID: testSessionID, SessionName: "", Resume: false},
			name:    "session only",
			wantErr: "",
		},
		{
			options: promptRunOptions{SessionID: "", SessionName: "", Resume: true},
			name:    "resume only",
			wantErr: "",
		},
		{
			options: promptRunOptions{SessionID: testSessionID, SessionName: "", Resume: true},
			name:    "resume with session",
			wantErr: "--resume cannot be used with --session",
		},
		{
			options: promptRunOptions{SessionID: "", SessionName: "named", Resume: true},
			name:    "resume with name",
			wantErr: "--resume cannot be used with --name",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validatePromptRunOptions(testCase.options)
			if testCase.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestPromptMessage(t *testing.T) {
	t.Parallel()

	const testHello = "hello"

	tests := []struct {
		name    string
		stdin   string
		want    string
		wantErr string
		args    []string
	}{
		{name: "args", args: []string{testHello, "world"}, stdin: "ignored", want: "hello world", wantErr: ""},
		{name: "trim args", args: []string{"  hello  "}, stdin: "ignored", want: testHello, wantErr: ""},
		{name: "stdin", args: nil, stdin: testHello + "\n", want: testHello, wantErr: ""},
		{name: "empty stdin", args: nil, stdin: "  \n", want: "", wantErr: "prompt message is required"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			cmd.SetIn(strings.NewReader(testCase.stdin))

			message, err := promptMessage(cmd, testCase.args)
			if testCase.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, testCase.want, message)

				return
			}

			require.Error(t, err)
			assert.Empty(t, message)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestPromptMessageRejectsStdinAboveLimit(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(strings.Repeat("a", int(promptStdinLimitBytes)+1)))

	message, err := promptMessage(cmd, nil)
	require.Error(t, err)
	assert.Empty(t, message)
	assert.Contains(t, err.Error(), "prompt stdin exceeds limit")
}
