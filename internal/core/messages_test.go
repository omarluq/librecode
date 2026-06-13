package core_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

const pwdCommand = "pwd"

func TestBashExecutionToText(t *testing.T) {
	t.Parallel()

	exitCode := 2

	tests := []struct {
		name     string
		contains []string
		message  core.BashExecutionMessage
	}{
		{
			name: "output",
			message: core.BashExecutionMessage{
				ExitCode:           nil,
				Command:            pwdCommand,
				Output:             "/tmp",
				FullOutputPath:     "",
				Timestamp:          0,
				Canceled:           false,
				Truncated:          false,
				ExcludeFromContext: false,
			},
			contains: []string{"Ran `pwd`", "```\n/tmp\n```"},
		},
		{
			name: "no output canceled",
			message: core.BashExecutionMessage{
				ExitCode:           nil,
				Command:            "sleep 10",
				Output:             "",
				FullOutputPath:     "",
				Timestamp:          0,
				Canceled:           true,
				Truncated:          false,
				ExcludeFromContext: false,
			},
			contains: []string{"(no output)", "command canceled"},
		},
		{
			name: "non zero truncated",
			message: core.BashExecutionMessage{
				ExitCode:           &exitCode,
				Command:            "fail",
				Output:             "boom",
				FullOutputPath:     "/tmp/output.log",
				Timestamp:          0,
				Canceled:           false,
				Truncated:          true,
				ExcludeFromContext: false,
			},
			contains: []string{"Command exited with code 2", "Full output: /tmp/output.log"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			text := core.BashExecutionToText(testCase.message)
			for _, fragment := range testCase.contains {
				assert.Contains(t, text, fragment)
			}
		})
	}
}

func TestMessageConstructorsAndLLMConversions(t *testing.T) {
	t.Parallel()

	branch := core.NewBranchSummaryMessage("branch summary", "branch-1", "2026-06-09T00:00:00.123Z")
	assert.Equal(t, int64(1780963200123), branch.Timestamp)
	assert.Equal(t, "branch-1", branch.FromID)

	compaction := core.NewCompactionSummaryMessage("compact summary", 42, "bad timestamp")
	assert.Zero(t, compaction.Timestamp)
	assert.Equal(t, 42, compaction.TokensBefore)

	custom := core.NewCustomMessage(
		"notice",
		[]core.ContentPart{{Type: "text", Text: "hello", Data: "", MIMEType: ""}},
		true,
		map[string]string{"k": "v"},
		"2026-06-09T00:00:00Z",
	)
	assert.Equal(t, "notice", custom.CustomType)
	assert.True(t, custom.Display)

	branchLLM := core.BranchSummaryToLLM(branch)
	require.Len(t, branchLLM.Content, 1)
	assert.Contains(t, branchLLM.Content[0].Text, core.BranchSummaryPrefix)
	assert.Contains(t, branchLLM.Content[0].Text, "branch summary")

	compactionLLM := core.CompactionSummaryToLLM(compaction)
	require.Len(t, compactionLLM.Content, 1)
	assert.Contains(t, compactionLLM.Content[0].Text, core.CompactionSummaryPrefix)
	assert.Contains(t, compactionLLM.Content[0].Text, "compact summary")
}

func TestBashExecutionToLLM(t *testing.T) {
	t.Parallel()

	message, included := core.BashExecutionToLLM(core.BashExecutionMessage{
		ExitCode:           nil,
		Command:            pwdCommand,
		Output:             "/tmp",
		FullOutputPath:     "",
		Timestamp:          123,
		Canceled:           false,
		Truncated:          false,
		ExcludeFromContext: false,
	})
	require.True(t, included)
	assert.Equal(t, "user", message.Role)
	assert.Equal(t, int64(123), message.Timestamp)
	require.Len(t, message.Content, 1)
	assert.Contains(t, message.Content[0].Text, "Ran `pwd`")

	message, included = core.BashExecutionToLLM(core.BashExecutionMessage{
		ExitCode:           nil,
		Command:            pwdCommand,
		Output:             "",
		FullOutputPath:     "",
		Timestamp:          0,
		Canceled:           false,
		Truncated:          false,
		ExcludeFromContext: true,
	})
	assert.False(t, included)
	assert.Empty(t, message.Content)
}
