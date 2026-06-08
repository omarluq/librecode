package assistant

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
)

func TestModelFacingMessagesPreservesModelFacingCustomRoles(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{
		newRuntimeContextTestMessage(database.RoleCustom, "extension context"),
		newRuntimeContextTestMessage(database.RoleBashExecution, "bash context"),
		newRuntimeContextTestMessage(database.RoleThinking, "thinking stays hidden"),
		newRuntimeContextTestMessage(database.RoleToolResult, "tool stays hidden"),
		newRuntimeContextTestMessage(database.RoleCompactionSummary, "earlier summary"),
		newRuntimeContextTestMessage(database.RoleBranchSummary, "branch summary"),
	}

	filtered := modelFacingMessages(messages)

	assert.Equal(t, []database.Role{
		database.RoleCustom,
		database.RoleBashExecution,
		database.RoleUser,
		database.RoleUser,
	}, messageRoles(filtered))
	assert.Equal(t, "extension context", filtered[0].Content)
	assert.Equal(t, "bash context", filtered[1].Content)
	assert.True(t, strings.HasPrefix(filtered[2].Content, core.CompactionSummaryPrefix))
	assert.Contains(t, filtered[2].Content, "earlier summary")
	assert.True(t, strings.HasPrefix(filtered[3].Content, core.BranchSummaryPrefix))
	assert.Contains(t, filtered[3].Content, "branch summary")
}

func newRuntimeContextTestMessage(role database.Role, content string) database.MessageEntity {
	return database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	}
}

func messageRoles(messages []database.MessageEntity) []database.Role {
	roles := make([]database.Role, 0, len(messages))
	for index := range messages {
		roles = append(roles, messages[index].Role)
	}

	return roles
}
