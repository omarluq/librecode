package database_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionDeletionTaskCascadeBehavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		deleteChild    bool
		wantParent     bool
		wantChild      bool
		wantTask       bool
		wantTaskEvents int
	}{
		{name: "deleting owner removes task and preserves child session", deleteChild: false, wantParent: false,
			wantChild: true, wantTask: false, wantTaskEvents: 0},
		{name: "deleting agent child removes task and preserves owner session", deleteChild: true, wantParent: true,
			wantChild: false, wantTask: false, wantTaskEvents: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newTaskTestFixture(t)
			ctx, agents, tasks, sessions := fixture.ctx, fixture.agents, fixture.tasks, fixture.sessions
			parent, child := fixture.createAgentTaskSessions()
			created, err := agents.Create(ctx, newAgentTask(parent.ID, child.ID))
			require.NoError(t, err)
			_, err = tasks.AppendEvent(ctx, created.Task.ID, "progress", `{}`)
			require.NoError(t, err)

			deletedID := parent.ID
			if test.deleteChild {
				deletedID = child.ID
			}

			require.NoError(t, sessions.DeleteSession(ctx, deletedID))

			_, taskFound, err := tasks.Get(ctx, created.Task.ID)
			require.NoError(t, err)
			assert.Equal(t, test.wantTask, taskFound)

			_, agentFound, err := agents.Get(ctx, created.Task.ID)
			require.NoError(t, err)
			assert.Equal(t, test.wantTask, agentFound)

			events, err := tasks.ListEvents(ctx, created.Task.ID, 0, 10)
			require.NoError(t, err)
			assert.Len(t, events, test.wantTaskEvents)

			_, parentFound, err := sessions.GetSession(ctx, parent.ID)
			require.NoError(t, err)
			assert.Equal(t, test.wantParent, parentFound)

			_, childFound, err := sessions.GetSession(ctx, child.ID)
			require.NoError(t, err)
			assert.Equal(t, test.wantChild, childFound)
		})
	}
}
