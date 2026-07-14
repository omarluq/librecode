package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestWatchAgentTaskPostsTerminalChange(t *testing.T) {
	t.Parallel()

	const taskID = "task-1"

	screen := newClipboardScreen()
	app := newRenderTestApp(t)
	app.screen = screen

	events := make(chan database.TaskEventEntity, 1)
	cancelCalled := make(chan struct{}, 1)

	events <- database.TaskEventEntity{
		Event: database.EventEntity{
			CreatedAt:   time.Time{},
			ID:          "event-1",
			Kind:        "task_succeeded",
			PayloadJSON: `{}`,
		},
		TaskID:   taskID,
		Sequence: 4,
	}

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	go app.watchAgentTask(ctx, taskID, events, func() { cancelCalled <- struct{}{} })

	var event tcell.Event
	select {
	case event = <-screen.EventQ():
	case <-ctx.Done():
		require.FailNow(t, "watch did not post terminal task event")
	}

	interrupt, ok := event.(*tcell.EventInterrupt)
	require.True(t, ok)
	payload, ok := interrupt.Data().(*asyncEvent)
	require.True(t, ok)
	assert.Equal(t, asyncEventAgentTaskChanged, payload.Kind)
	assert.Equal(t, taskID, payload.Text)

	select {
	case <-cancelCalled:
	case <-ctx.Done():
		require.FailNow(t, "watch did not cancel its subscription")
	}
}
