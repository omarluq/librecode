package assistant

import (
	"context"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/database"
)

func TestRuntimePromptParentIDRequiresValidSessionEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := agentToolSessions(t)
	session, err := repository.CreateSession(ctx, t.TempDir(), "prompt endpoint", "")
	require.NoError(t, err)
	otherSession, err := repository.CreateSession(ctx, t.TempDir(), "other", "")
	require.NoError(t, err)

	root := appendRuntimeContextTestMessage(t, repository, session.ID, nil, database.RoleUser, "root")
	foreign := appendRuntimeContextTestMessage(t, repository, otherSession.ID, nil, database.RoleUser, "foreign")

	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Sessions = repository
	})

	blank := ""
	tests := []struct {
		name     string
		parent   *string
		wantID   string
		wantCode string
	}{
		{name: "nil parent resolves the session leaf", parent: nil, wantID: root.ID, wantCode: ""},
		{name: "blank explicit parent resolves the session root", parent: &blank, wantID: root.ID, wantCode: ""},
		{name: "valid endpoint is returned unchanged", parent: &root.ID, wantID: root.ID, wantCode: ""},
		{name: "missing endpoint is rejected", parent: new("not-an-entry"), wantID: "",
			wantCode: "prompt_parent_not_found"},
		{name: "cross-session endpoint is rejected", parent: &foreign.ID, wantID: "",
			wantCode: "prompt_parent_not_found"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parentID, err := runtime.promptParentID(ctx, session.ID, testCase.parent)

			if testCase.wantCode == "" {
				require.NoError(t, err)
				require.NotNil(t, parentID)
				assert.Equal(t, testCase.wantID, *parentID)

				return
			}

			require.Nil(t, parentID)
			require.Error(t, err)
			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, testCase.wantCode, oopsErr.Code())
		})
	}
}

func TestRuntimePromptRejectsForeignParentEndpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repository := agentToolSessions(t)
	session, err := repository.CreateSession(ctx, t.TempDir(), "prompt foreign", "")
	require.NoError(t, err)
	otherSession, err := repository.CreateSession(ctx, t.TempDir(), "other", "")
	require.NoError(t, err)
	foreign := appendRuntimeContextTestMessage(t, repository, otherSession.ID, nil, database.RoleUser, "foreign")

	runtime := newRuntimeFromDeps(func(deps *runtimeDeps) {
		deps.Sessions = repository
		deps.Config = new(config.Config)
	})

	request := &PromptRequest{
		OnEvent:          nil,
		OnRetry:          nil,
		OnUserEntry:      nil,
		OnSteeringReturn: nil,
		ParentEntryID:    &foreign.ID,
		SessionID:        session.ID,
		CWD:              t.TempDir(),
		Images:           nil,
		Text:             adapterHello,
		Name:             "",
		ResumeLatest:     false,
		HideUserPrompt:   false,
	}

	response, err := runtime.Prompt(ctx, request)

	require.Nil(t, response)
	require.Error(t, err)
	oopsErr, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "prompt_parent_not_found", oopsErr.Code())
}
