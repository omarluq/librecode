package agenttask

import (
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

// writerFixture wires a real task repository so writer flushes are durable
// and observable through ListEvents.
type writerFixture struct {
	service *Service
	tasks   *database.TaskRepository
	taskID  string
}

func newWriterFixture(t *testing.T) writerFixture {
	t.Helper()

	fixture := newServiceRepositoryFixture(t)
	created := fixture.createQueuedAgentTask(t)

	claimed, err := fixture.tasks.ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: created.Task.ID, LeaseOwner: workerName,
		LeaseExpiresAt: time.Now().Add(time.Minute), EventKind: "task_started",
	})
	require.NoError(t, err)
	require.True(t, claimed)

	service := serviceWithRepositories(fixture.tasks, fixture.agentTasks)
	service.leaseOwner = workerName

	return writerFixture{service: service, tasks: fixture.tasks, taskID: created.Task.ID}
}

func (fixture writerFixture) events(t *testing.T) []database.TaskEventEntity {
	t.Helper()

	events, err := fixture.tasks.ListEvents(t.Context(), fixture.taskID, 0, 1000)
	require.NoError(t, err)

	return events
}

// delta builds a coalescible stream delta event.
func delta(kind assistant.StreamEventKind, text string) assistant.StreamEvent {
	return assistant.StreamEvent{
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: kind, Text: text,
	}
}

func TestTaskEventWriterCoalescesStreamDeltas(t *testing.T) {
	t.Parallel()

	fixture := newWriterFixture(t)
	writer := fixture.service.newTaskEventWriter(fixture.taskID)
	writer.flushInterval = time.Hour
	writer.start(t.Context()) // disable the ticker; only batch/close flush

	for range eventFlushBatch - 1 {
		require.NoError(t, writer.write(t.Context(), string(assistant.StreamEventTextDelta),
			delta(assistant.StreamEventTextDelta, "x")))
	}

	// Below the batch threshold nothing beyond the claim events is durable.
	assert.Len(t, fixture.events(t), 2, "deltas must stay buffered until flush")

	// The final delta crosses the batch threshold and flushes all of them.
	require.NoError(t, writer.write(t.Context(), string(assistant.StreamEventTextDelta),
		delta(assistant.StreamEventTextDelta, "x")))

	persisted := fixture.events(t)
	require.Len(t, persisted, eventFlushBatch+2, "queued + started + flushed batch")

	for index := range eventFlushBatch {
		assert.Equal(t, string(assistant.StreamEventTextDelta), persisted[index+2].Event.Kind)
	}

	require.NoError(t, writer.close(t.Context()))
}

func TestTaskEventWriterFlushesStructuralEventsImmediately(t *testing.T) {
	t.Parallel()

	fixture := newWriterFixture(t)
	writer := fixture.service.newTaskEventWriter(fixture.taskID)
	writer.flushInterval = time.Hour
	writer.start(t.Context())

	require.NoError(t, writer.write(t.Context(), string(assistant.StreamEventTextDelta),
		delta(assistant.StreamEventTextDelta, "partial")))
	require.NoError(t, writer.write(t.Context(), string(assistant.StreamEventToolResult),
		delta(assistant.StreamEventToolResult, "done")))

	persisted := fixture.events(t)
	require.Len(t, persisted, 4, "queued + started + buffered delta + structural event")
	assert.Equal(t, string(assistant.StreamEventTextDelta), persisted[2].Event.Kind)
	assert.Equal(t, string(assistant.StreamEventToolResult), persisted[3].Event.Kind)

	require.NoError(t, writer.close(t.Context()))
}

func TestTaskEventWriterTickerFlush(t *testing.T) {
	t.Parallel()

	fixture := newWriterFixture(t)
	writer := fixture.service.newTaskEventWriter(fixture.taskID)
	writer.flushInterval = 20 * time.Millisecond
	writer.start(t.Context())

	require.NoError(t, writer.write(t.Context(), string(assistant.StreamEventTextDelta),
		delta(assistant.StreamEventTextDelta, "x")))

	require.Eventually(t, func() bool {
		return len(fixture.events(t)) > 2
	}, time.Second, 5*time.Millisecond, "ticker must flush buffered deltas")

	require.NoError(t, writer.close(t.Context()))
}

func TestTaskEventWriterCloseFlushesRemaining(t *testing.T) {
	t.Parallel()

	fixture := newWriterFixture(t)
	writer := fixture.service.newTaskEventWriter(fixture.taskID)
	writer.flushInterval = time.Hour
	writer.start(t.Context())

	require.NoError(t, writer.write(t.Context(), string(assistant.StreamEventTextDelta),
		delta(assistant.StreamEventTextDelta, "tail")))

	require.NoError(t, writer.close(t.Context()))
	assert.Len(t, fixture.events(t), 3, "close must flush buffered deltas")

	// Writes after close fail closed.
	require.ErrorContains(t, writer.write(t.Context(), "event", map[string]string{}),
		"task event writer is closed")

	// Double close is safe and idempotent.
	require.NoError(t, writer.close(t.Context()))
}

func TestTaskEventWriterFailsClosedOnLeaseLoss(t *testing.T) {
	t.Parallel()

	fixture := newWriterFixture(t)
	writer := fixture.service.newTaskEventWriter(fixture.taskID)
	writer.flushInterval = time.Hour
	writer.start(t.Context())

	// Steal the lease: finish the task out from under the writer.
	changed, err := fixture.tasks.Finish(t.Context(), &database.TaskFinish{
		TaskID: fixture.taskID, From: []database.TaskState{database.TaskRunning},
		TargetState: database.TaskCanceled, EventKind: "task_canceled", Result: "",
		ErrorCode: "canceled", ErrorMessage: "canceled", LeaseOwner: workerName,
		PayloadJSON: `{}`,
	})
	require.NoError(t, err)
	require.True(t, changed)

	require.NoError(t, writer.write(t.Context(), string(assistant.StreamEventTextDelta),
		delta(assistant.StreamEventTextDelta, "x")))

	// The flush at close discovers the lost lease and fails closed.
	err = writer.close(t.Context())
	require.ErrorContains(t, err, "lease lost")

	// The rejected delta must not be durable: queued + started + terminal only.
	assert.Len(t, fixture.events(t), 3)

	// Once the error is recorded, further writes fail fast without touching the database.
	assert.ErrorContains(t, writer.write(t.Context(), string(assistant.StreamEventTextDelta),
		delta(assistant.StreamEventTextDelta, "x")), "lease lost")
}

func TestTaskEventWriterDiscardsPendingDraftsAfterFailure(t *testing.T) {
	t.Parallel()

	fixture := newWriterFixture(t)
	writer := fixture.service.newTaskEventWriter(fixture.taskID)
	writer.flushInterval = time.Hour
	writer.start(t.Context())

	// Simulate a write that raced the first flush failure: it passed the
	// error check before recordErr ran and queued a draft afterwards.
	writer.mu.Lock()
	writer.err = oops.In("agenttask").Code("task_lease_lost").
		Wrapf(errTaskLeaseLost, "stop writing task events")
	writer.pending = []database.TaskEventDraft{{
		Kind: string(assistant.StreamEventTextDelta), PayloadJSON: `{"text":"raced"}`,
	}}
	writer.mu.Unlock()

	// Later ticker or close flushes must discard the draft, never persist it.
	writer.flush(t.Context())

	err := writer.close(t.Context())
	require.ErrorContains(t, err, "lease lost")

	// queued + started only: the raced draft must not become durable.
	assert.Len(t, fixture.events(t), 2)
}
