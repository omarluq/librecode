package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSessionName = "named"

func TestValidatePromptRunOptions(t *testing.T) {
	t.Parallel()

	const testSessionID = "session-1"

	tests := []struct {
		wantErr string
		name    string
		options promptRunOptions
	}{
		{
			options: promptRunOptions{SessionID: "", SessionName: "", ToolStrategy: "", MetricsJSON: "", Resume: false},
			name:    "default",
			wantErr: "",
		},
		{
			options: promptRunOptions{
				SessionID: testSessionID, SessionName: "", ToolStrategy: "", MetricsJSON: "", Resume: false,
			},
			name:    "session only",
			wantErr: "",
		},
		{
			options: promptRunOptions{SessionID: "", SessionName: "", ToolStrategy: "", MetricsJSON: "", Resume: true},
			name:    "resume only",
			wantErr: "",
		},
		{
			options: promptRunOptions{
				SessionID: testSessionID, SessionName: "", ToolStrategy: "", MetricsJSON: "", Resume: true,
			},
			name:    "resume with session",
			wantErr: "--resume cannot be used with --session",
		},
		{
			options: promptRunOptions{
				SessionID: "", SessionName: testSessionName, ToolStrategy: "hybrid", MetricsJSON: "", Resume: true,
			},
			name:    "resume with name",
			wantErr: "--resume cannot be used with --name",
		},
		{
			options: promptRunOptions{
				SessionID: "", SessionName: "", ToolStrategy: "invalid", MetricsJSON: "", Resume: false,
			},
			name:    "invalid strategy",
			wantErr: "invalid --tool-strategy",
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

func TestNewPromptCmdRejectsConflictingSessionFlags(t *testing.T) {
	t.Parallel()

	const testHello = "hello"

	tests := []struct {
		name    string
		wantErr string
		args    []string
	}{
		{
			name:    "resume and session",
			wantErr: "--resume cannot be used with --session",
			args:    []string{"--resume", "--session", "session-1", testHello},
		},
		{
			name:    "resume and name",
			wantErr: "--resume cannot be used with --name",
			args:    []string{"--resume", "--name", testSessionName, testHello},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := newPromptCmd()
			cmd.SetArgs(testCase.args)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true

			err := cmd.Execute()
			require.ErrorContains(t, err, testCase.wantErr)
		})
	}
}

type failingPromptReader struct{}

func (failingPromptReader) Read([]byte) (int, error) {
	return 0, errors.New("stdin unavailable")
}

func TestPromptMessageWrapsStdinReadError(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.SetIn(failingPromptReader{})

	message, err := promptMessage(cmd, nil)
	require.ErrorContains(t, err, "read stdin")
	require.ErrorContains(t, err, "stdin unavailable")
	assert.Empty(t, message)
}

func TestBuildPromptRequest(t *testing.T) {
	t.Parallel()

	request := buildPromptRequest("/workspace", "hello", promptRunOptions{
		SessionID: "session-1", SessionName: testSessionName, ToolStrategy: "", MetricsJSON: "", Resume: true,
	})

	require.NotNil(t, request)
	assert.Equal(t, "session-1", request.SessionID)
	assert.Equal(t, "/workspace", request.CWD)
	assert.Equal(t, "hello", request.Text)
	assert.Equal(t, testSessionName, request.Name)
	assert.True(t, request.ResumeLatest)
	assert.Nil(t, request.OnEvent)
	assert.Nil(t, request.OnRetry)
	assert.Nil(t, request.OnUserEntry)
	assert.Nil(t, request.ParentEntryID)
	assert.False(t, request.HideUserPrompt)
}

func TestWritePromptMetrics(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "metrics.json")
	metrics := &promptMetrics{
		Strategy: "direct", Error: "", ProviderRoundTrips: 2, ElapsedMilliseconds: 12,
		InputTokens: 10, OutputTokens: 4, ToolCalls: 1, NestedToolCalls: 0,
		TraceComplete: true, Success: true,
	}
	require.NoError(t, writePromptMetrics(path, metrics))

	encoded, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)

	var decoded promptMetrics
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	assert.Equal(t, *metrics, decoded)
}

func TestNormalizedToolStrategyDefaultsToHybrid(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hybrid", string(normalizedToolStrategy("")))
	assert.Equal(t, "direct", string(normalizedToolStrategy("direct")))
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
