package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
	"github.com/omarluq/librecode/internal/database"
)

func TestRuntimeModelContextEntityFromUsesExplicitBranch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := agentToolSessions(t)
	session, err := repository.CreateSession(ctx, t.TempDir(), "context branch", "")
	require.NoError(t, err)

	root := appendRuntimeContextTestMessage(t, repository, session.ID, nil, database.RoleUser, "root")
	left := appendRuntimeContextTestMessage(t, repository, session.ID, &root.ID, database.RoleAssistant, "left")
	appendRuntimeContextTestMessage(t, repository, session.ID, &root.ID, database.RoleAssistant, "newer right")

	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Sessions = repository
	})
	contextEntity, err := runtime.modelContextEntityFrom(ctx, session.ID, left.ID)

	require.NoError(t, err)
	require.Len(t, contextEntity.Messages, 2)
	assert.Equal(t, []string{"root", "left"}, []string{
		contextEntity.Messages[0].Content,
		contextEntity.Messages[1].Content,
	})
}

func TestRuntimeModelContextEntityFromRejectsInvalidEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := agentToolSessions(t)
	firstSession, err := repository.CreateSession(ctx, t.TempDir(), "first", "")
	require.NoError(t, err)
	secondSession, err := repository.CreateSession(ctx, t.TempDir(), "second", "")
	require.NoError(t, err)
	foreignEntry := appendRuntimeContextTestMessage(
		t,
		repository,
		secondSession.ID,
		nil,
		database.RoleUser,
		"foreign",
	)
	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Sessions = repository
	})

	tests := []struct {
		name     string
		entryID  string
		wantCode string
	}{
		{name: "blank endpoint", entryID: " ", wantCode: "context_entry_required"},
		{name: "cross-session endpoint", entryID: foreignEntry.ID, wantCode: "branch_entry_missing"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := runtime.modelContextEntityFrom(ctx, firstSession.ID, testCase.entryID)

			require.Error(t, err)
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, testCase.wantCode, oopsErr.Code())
		})
	}
}

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

func appendRuntimeContextTestMessage(
	t *testing.T,
	repository *database.SessionRepository,
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
) *database.EntryEntity {
	t.Helper()

	message := newRuntimeContextTestMessage(role, content)
	entry, err := repository.AppendMessage(context.Background(), sessionID, parentID, &message)
	require.NoError(t, err)

	return entry
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
