package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func TestIsFacingRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role database.Role
		name string
		want bool
	}{
		{name: "user", role: database.RoleUser, want: true},
		{name: "assistant", role: database.RoleAssistant, want: true},
		{name: "branch summary", role: database.RoleBranchSummary, want: true},
		{name: "compaction summary", role: database.RoleCompactionSummary, want: true},
		{name: "custom", role: database.RoleCustom, want: true},
		{name: "bash execution", role: database.RoleBashExecution, want: true},
		{name: "tool result", role: database.RoleToolResult, want: false},
		{name: "thinking", role: database.RoleThinking, want: false},
		{name: "unknown", role: database.Role("other"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, model.IsFacingRole(test.role))
		})
	}
}

func TestFacingMessageWrapsSummaryRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		role        database.Role
		name        string
		wantRole    database.Role
		wantContent string
	}{
		{
			name:        "compaction summary",
			role:        database.RoleCompactionSummary,
			wantRole:    database.RoleUser,
			wantContent: core.CompactionSummaryPrefix + "compacted facts" + core.CompactionSummarySuffix,
		},
		{
			name:        "branch summary",
			role:        database.RoleBranchSummary,
			wantRole:    database.RoleUser,
			wantContent: core.BranchSummaryPrefix + "compacted facts" + core.BranchSummarySuffix,
		},
		{
			name:        "assistant passthrough",
			role:        database.RoleAssistant,
			wantRole:    database.RoleAssistant,
			wantContent: "compacted facts",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			message := testMessage(test.role, "compacted facts")
			converted := model.FacingMessage(&message)

			assert.Equal(t, test.wantRole, converted.Role)
			assert.Equal(t, test.wantContent, converted.Content)
			assert.Equal(t, message.Timestamp, converted.Timestamp)
		})
	}
}

func TestFacingMessagesFiltersAndConverts(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{
		testMessage(database.RoleUser, "hello"),
		testMessage(database.RoleAssistant, "   "),
		testMessage(database.RoleThinking, "hidden thoughts"),
		testMessage(database.RoleBranchSummary, "branch facts"),
		testMessage(database.RoleToolResult, "tool output"),
		testMessage(database.RoleCustom, "custom facts"),
	}

	facing := model.FacingMessages(messages)

	require.Len(t, facing, 3)
	assert.Equal(t, database.RoleUser, facing[0].Role)
	assert.Equal(t, "hello", facing[0].Content)
	assert.Equal(t, database.RoleUser, facing[1].Role)
	assert.Equal(t, core.BranchSummaryPrefix+"branch facts"+core.BranchSummarySuffix, facing[1].Content)
	assert.Equal(t, database.RoleCustom, facing[2].Role)
	assert.Equal(t, "custom facts", facing[2].Content)
}

func TestIsFacingMessageHandlesNilAndBlankContent(t *testing.T) {
	t.Parallel()

	blank := testMessage(database.RoleUser, " \n\t ")
	toolResult := testMessage(database.RoleToolResult, "visible text")
	user := testMessage(database.RoleUser, "visible text")

	assert.False(t, model.IsFacingMessage(nil))
	assert.False(t, model.IsFacingMessage(&blank))
	assert.False(t, model.IsFacingMessage(&toolResult))
	assert.True(t, model.IsFacingMessage(&user))
}

func testMessage(role database.Role, content string) database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Unix(123, 0).UTC(),
		Role:      role,
		Content:   content,
		Provider:  "provider",
		Model:     "model",
	}
}
