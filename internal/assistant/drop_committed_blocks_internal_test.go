package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/transcript"
)

func TestDropCommittedBlocksDropsSeparatedAssistantBlocks(t *testing.T) {
	t.Parallel()

	blocks := []partialPromptBlock{
		{Role: transcript.RoleAssistant, Content: "first text"},
		{Role: transcript.RoleThinking, Content: "thought"},
		{Role: transcript.RoleToolResult, Content: "tool result"},
		{Role: transcript.RoleAssistant, Content: "second text"},
		{Role: transcript.RoleCustom, Content: "keep"},
	}

	remaining := dropCommittedBlocks(blocks, 1, "first text\nsecond text", 1)

	assert.Equal(t, []partialPromptBlock{{Role: transcript.RoleCustom, Content: "keep"}}, remaining)
}
