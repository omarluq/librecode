package agenttask

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
)

// errTaskLeaseLost stops event writing once the task lease is no longer owned.
var errTaskLeaseLost = errors.New("agent task lease lost")

// taskEventWriter coalesces streamed task events into batched lease-fenced
// database appends. Stream deltas are buffered and flushed on a bounded
// ticker, a batch-size threshold, or any structural event kind, so one task
// no longer performs a write transaction per provider delta.
type taskEventWriter struct {
	err           error
	service       *Service
	stop          chan struct{}
	done          chan struct{}
	taskID        string
	pending       []database.TaskEventDraft
	flushInterval time.Duration
	flushMu       sync.Mutex
	mu            sync.Mutex
	flushBatch    int
	closed        bool
}

// newTaskEventWriter creates the coalescing writer for one running task.
// The flush loop only starts with start, so callers may override the flush
// tuning fields first.
func (service *Service) newTaskEventWriter(taskID string) *taskEventWriter {
	return &taskEventWriter{
		service: service, taskID: taskID,
		flushInterval: eventFlushInterval, flushBatch: eventFlushBatch,
		done: make(chan struct{}), stop: make(chan struct{}),
		flushMu: sync.Mutex{}, mu: sync.Mutex{},
		pending: nil, err: nil, closed: false,
	}
}

// start launches the flush ticker goroutine scoped to the run context.
func (writer *taskEventWriter) start(runCtx context.Context) {
	go writer.run(runCtx)
}

// sink adapts the writer to the runner event sink contract.
func (writer *taskEventWriter) sink() EventSink {
	return writer.write
}

// close stops the ticker, flushes remaining events, and reports the first
// write failure observed by the writer. The run context may already be
// canceled; the final flush still deserves a bounded attempt because the
// lease is checked at append time.
func (writer *taskEventWriter) close(runCtx context.Context) error {
	writer.mu.Lock()
	if writer.closed {
		err := writer.err
		writer.mu.Unlock()

		return err
	}

	writer.closed = true
	writer.mu.Unlock()

	close(writer.stop)
	<-writer.done

	ctx, cancel := context.WithTimeout(context.WithoutCancel(runCtx), finalizeTimeout)
	defer cancel()

	writer.flush(ctx)

	writer.mu.Lock()
	defer writer.mu.Unlock()

	return writer.err
}

func (writer *taskEventWriter) run(runCtx context.Context) {
	defer close(writer.done)

	ticker := time.NewTicker(writer.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-writer.stop:
			return
		case <-runCtx.Done():
			return
		case <-ticker.C:
			writer.flush(runCtx)
		}
	}
}

func (writer *taskEventWriter) write(ctx context.Context, kind string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return oops.In("agenttask").Code("marshal_event").Wrapf(err, "marshal task event")
	}

	writer.mu.Lock()
	if err := writer.err; err != nil {
		writer.mu.Unlock()

		return err
	}

	if writer.closed {
		writer.mu.Unlock()

		return oops.In("agenttask").Code("event_writer_closed").Errorf("task event writer is closed")
	}

	writer.pending = append(writer.pending, database.TaskEventDraft{
		Kind: kind, PayloadJSON: string(encoded),
	})
	force := len(writer.pending) >= writer.flushBatch || !isStreamDeltaKind(kind)
	writer.mu.Unlock()

	if force {
		writer.flush(ctx)
	}

	return writer.loadErr()
}

// flush swaps the pending batch and appends it in one lease-fenced
// transaction. flushMu serializes flushes so concurrent writers cannot
// reorder durable sequences.
func (writer *taskEventWriter) flush(ctx context.Context) {
	writer.flushMu.Lock()
	defer writer.flushMu.Unlock()

	writer.mu.Lock()
	if writer.err != nil {
		writer.pending = nil
		writer.mu.Unlock()

		return
	}

	pending := writer.pending
	writer.pending = nil
	writer.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	events, appended, err := writer.service.tasks.AppendRunningEvents(
		ctx, writer.taskID, writer.service.leaseOwner, pending,
	)
	if err != nil {
		writer.recordErr(oops.In("agenttask").Code("append_event").Wrapf(err, "append task events"))

		return
	}

	if !appended {
		writer.recordErr(oops.In("agenttask").Code("task_lease_lost").
			Wrapf(errTaskLeaseLost, "stop writing task events"))

		return
	}

	for index := range events {
		writer.service.publish(&events[index])
	}
}

func (writer *taskEventWriter) recordErr(err error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.err == nil {
		writer.err = err
	}
}

func (writer *taskEventWriter) loadErr() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	return writer.err
}

// isStreamDeltaKind reports whether an event is a high-frequency provider
// delta that may be coalesced. Every other kind is structural and must be
// persisted (with any buffered deltas) as soon as it occurs.
func isStreamDeltaKind(kind string) bool {
	return kind == string(assistant.StreamEventTextDelta) ||
		kind == string(assistant.StreamEventThinkingDelta)
}
