package transcript_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/transcript"
)

func TestRoleDatabaseAdapters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		db   database.Role
		role transcript.Role
	}{
		{name: "user", db: database.RoleUser, role: transcript.RoleUser},
		{name: "assistant", db: database.RoleAssistant, role: transcript.RoleAssistant},
		{name: "tool result", db: database.RoleToolResult, role: transcript.RoleToolResult},
		{name: "thinking", db: database.RoleThinking, role: transcript.RoleThinking},
		{name: "custom", db: database.RoleCustom, role: transcript.RoleCustom},
		{name: "bash execution", db: database.RoleBashExecution, role: transcript.RoleBashExecution},
		{name: "branch summary", db: database.RoleBranchSummary, role: transcript.RoleBranchSummary},
		{name: "compaction summary", db: database.RoleCompactionSummary, role: transcript.RoleCompactionSummary},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.role, transcript.FromDatabaseRole(test.db))
			assert.Equal(t, test.db, transcript.ToDatabaseRole(test.role))
		})
	}
}

func TestCanMergeStreamingRole(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role transcript.Role
		want bool
	}{
		{name: "assistant", role: transcript.RoleAssistant, want: true},
		{name: "thinking", role: transcript.RoleThinking, want: true},
		{name: "user", role: transcript.RoleUser, want: false},
		{name: "tool", role: transcript.RoleToolResult, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, transcript.CanMergeStreamingRole(test.role))
		})
	}
}
