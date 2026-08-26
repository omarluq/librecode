package agenttask

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/taskprogress"
)

// taskEventWriter adapts the reusable leased-task progress writer to agent
// stream events.
type taskEventWriter struct {
	service       *Service
	writer        *taskprogress.Writer
	taskID        string
	flushInterval time.Duration
	flushBatch    int
}

// newTaskEventWriter creates the coalescing writer for one running task.
func (service *Service) newTaskEventWriter(taskID string) *taskEventWriter {
	return &taskEventWriter{
		service: service, taskID: taskID, flushInterval: eventFlushInterval,
		flushBatch: eventFlushBatch, writer: nil,
	}
}

func (writer *taskEventWriter) start(runCtx context.Context) {
	writer.progress().Start(runCtx)
}

func (writer *taskEventWriter) sink() EventSink { return writer.write }

func (writer *taskEventWriter) close(runCtx context.Context) error {
	err := writer.progress().Close(runCtx)
	if errors.Is(err, taskprogress.ErrLeaseLost) {
		return oops.In("agenttask").Code("task_lease_lost").Wrapf(err, "stop writing task events")
	}

	if err != nil {
		return oops.In("agenttask").Code("append_event").Wrapf(err, "append task events")
	}

	return nil
}

func (writer *taskEventWriter) write(ctx context.Context, kind string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return oops.In("agenttask").Code("marshal_event").Wrapf(err, "marshal task event")
	}

	err = writer.progress().Write(ctx, database.TaskEventDraft{Kind: kind, PayloadJSON: string(encoded)},
		!isStreamDeltaKind(kind))
	if errors.Is(err, taskprogress.ErrClosed) {
		return oops.In("agenttask").Code("event_writer_closed").Wrapf(err, "task event writer is closed")
	}

	if errors.Is(err, taskprogress.ErrLeaseLost) {
		return oops.In("agenttask").Code("task_lease_lost").Wrapf(err, "stop writing task events")
	}

	if err != nil {
		return oops.In("agenttask").Code("append_event").Wrapf(err, "append task events")
	}

	return nil
}

func (writer *taskEventWriter) progress() *taskprogress.Writer {
	if writer.writer == nil {
		writer.writer = taskprogress.New(func(ctx context.Context, drafts []database.TaskEventDraft) (
			[]database.TaskEventEntity, bool, error,
		) {
			return writer.service.tasks.AppendRunningEvents(ctx, writer.taskID, writer.service.leaseOwner, drafts)
		}, writer.service.publish, taskprogress.Options{
			FlushInterval: writer.flushInterval, FlushBatch: writer.flushBatch, FinalizeTimeout: finalizeTimeout,
		})
	}

	return writer.writer
}

func isStreamDeltaKind(kind string) bool {
	return kind == string(assistant.StreamEventTextDelta) ||
		kind == string(assistant.StreamEventThinkingDelta)
}
