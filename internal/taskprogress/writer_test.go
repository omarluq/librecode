package taskprogress_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/taskprogress"
)

func TestWriterOrdersBatchesAndPublishesAfterCommit(t *testing.T) {
	t.Parallel()

	var stateMutex sync.Mutex

	committed := false
	published := []int64{}
	writer := taskprogress.New(func(_ context.Context, drafts []database.TaskEventDraft) (
		[]database.TaskEventEntity, bool, error,
	) {
		stateMutex.Lock()
		committed = true
		stateMutex.Unlock()

		events := make([]database.TaskEventEntity, len(drafts))
		for index := range drafts {
			events[index].Sequence = int64(index + 1)
		}

		return events, true, nil
	}, func(event *database.TaskEventEntity) {
		stateMutex.Lock()
		defer stateMutex.Unlock()

		require.True(t, committed, "publish must follow commit")

		published = append(published, event.Sequence)
	}, taskprogress.Options{FlushInterval: time.Hour, FlushBatch: 3, FinalizeTimeout: time.Second})
	writer.Start(t.Context())

	require.NoError(t, writer.Write(t.Context(), database.TaskEventDraft{Kind: "delta", PayloadJSON: `{}`}, false))
	require.NoError(t, writer.Write(t.Context(), database.TaskEventDraft{Kind: "delta", PayloadJSON: `{}`}, false))
	assert.Empty(t, published)
	require.NoError(t, writer.Write(t.Context(), database.TaskEventDraft{Kind: "structural", PayloadJSON: `{}`}, true))
	assert.Equal(t, []int64{1, 2, 3}, published)
	require.NoError(t, writer.Close(t.Context()))
}

func TestWriterLeaseLossFailsClosedWithoutPublishing(t *testing.T) {
	t.Parallel()

	published := 0
	writer := taskprogress.New(func(context.Context, []database.TaskEventDraft) (
		[]database.TaskEventEntity, bool, error,
	) {
		return nil, false, nil
	}, func(*database.TaskEventEntity) { published++ }, taskprogress.Options{
		FlushInterval: time.Hour, FlushBatch: 0, FinalizeTimeout: 0,
	})

	err := writer.Write(t.Context(), database.TaskEventDraft{Kind: "structural", PayloadJSON: `{}`}, true)
	require.ErrorIs(t, err, taskprogress.ErrLeaseLost)
	assert.Zero(t, published)
	require.ErrorIs(t, writer.Write(t.Context(), database.TaskEventDraft{Kind: "later", PayloadJSON: `{}`}, true),
		taskprogress.ErrLeaseLost)
	require.ErrorIs(t, writer.Close(t.Context()), taskprogress.ErrLeaseLost)
}

func TestWriterCloseFlushesWithCanceledContextAndIsIdempotent(t *testing.T) {
	t.Parallel()

	var received []database.TaskEventDraft

	writer := taskprogress.New(func(ctx context.Context, drafts []database.TaskEventDraft) (
		[]database.TaskEventEntity, bool, error,
	) {
		require.NoError(t, ctx.Err())

		received = append(received, drafts...)

		return make([]database.TaskEventEntity, len(drafts)), true, nil
	}, nil, taskprogress.Options{FlushInterval: time.Hour, FlushBatch: 0, FinalizeTimeout: time.Second})
	writer.Start(t.Context())
	require.NoError(t, writer.Write(t.Context(), database.TaskEventDraft{Kind: "tail", PayloadJSON: `{}`}, false))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, writer.Close(ctx))
	require.Len(t, received, 1)
	require.NoError(t, writer.Close(ctx))
	require.ErrorIs(t, writer.Write(t.Context(), database.TaskEventDraft{Kind: "late", PayloadJSON: `{}`}, true),
		taskprogress.ErrClosed)
}

func TestWriterCancellationRetainsPendingForClose(t *testing.T) {
	t.Parallel()

	var received []database.TaskEventDraft

	attempts := 0
	writer := taskprogress.New(func(ctx context.Context, drafts []database.TaskEventDraft) (
		[]database.TaskEventEntity, bool, error,
	) {
		attempts++
		if attempts == 1 {
			return nil, false, ctx.Err()
		}

		received = append(received, drafts...)

		return make([]database.TaskEventEntity, len(drafts)), true, nil
	}, nil, taskprogress.Options{FlushInterval: 0, FlushBatch: 0, FinalizeTimeout: time.Second})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, writer.Write(ctx, database.TaskEventDraft{Kind: "event", PayloadJSON: `{}`}, true))
	require.NoError(t, writer.Close(ctx))
	assert.Equal(t, 2, attempts)
	require.Len(t, received, 1)
}

func TestWriterAppendErrorIsSticky(t *testing.T) {
	t.Parallel()

	want := errors.New("database unavailable")
	writer := taskprogress.New(func(context.Context, []database.TaskEventDraft) (
		[]database.TaskEventEntity, bool, error,
	) {
		return nil, false, want
	}, nil, taskprogress.Options{FlushInterval: 0, FlushBatch: 0, FinalizeTimeout: 0})
	err := writer.Write(t.Context(), database.TaskEventDraft{Kind: "event", PayloadJSON: `{}`}, true)
	require.ErrorIs(t, err, want)
	require.ErrorIs(t, writer.Close(t.Context()), want)
}
