// Package taskprogress batches durable progress events for leased tasks.
package taskprogress

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/omarluq/librecode/internal/database"
)

// ErrLeaseLost is recorded when a fenced append no longer owns the task lease.
var ErrLeaseLost = errors.New("task lease lost")

// ErrClosed is returned by writes after Close.
var ErrClosed = errors.New("progress writer is closed")

// Appender commits one ordered, lease-fenced batch. A false appended result
// means no events were committed because the lease is no longer owned.
type Appender func(context.Context, []database.TaskEventDraft) ([]database.TaskEventEntity, bool, error)

// Publisher receives committed events in durable sequence order.
type Publisher func(*database.TaskEventEntity)

// Options configures progress batching.
type Options struct {
	FlushInterval   time.Duration
	FlushBatch      int
	FinalizeTimeout time.Duration
}

// Writer serializes batches, publishes only committed events, and fails closed
// after an append error or lease loss.
type Writer struct {
	err     error
	append  Appender
	publish Publisher
	stop    chan struct{}
	done    chan struct{}
	pending []database.TaskEventDraft
	options Options
	once    sync.Once
	flushMu sync.Mutex
	mu      sync.Mutex
	closed  bool
}

// New creates a writer. Start enables periodic flushing; writes and Close are
// still safe when Start is not called.
func New(appendFn Appender, publish Publisher, options Options) *Writer {
	return &Writer{
		append: appendFn, publish: publish, options: options,
		stop: make(chan struct{}), done: make(chan struct{}), once: sync.Once{},
		flushMu: sync.Mutex{}, mu: sync.Mutex{}, pending: nil, err: nil, closed: false,
	}
}

// Start launches periodic flushing scoped to ctx.
func (writer *Writer) Start(ctx context.Context) {
	writer.once.Do(func() { go writer.run(ctx) })
}

// Write queues a draft and flushes when immediate is true or the batch limit
// is reached. Callers should mark structural and terminal progress immediate.
func (writer *Writer) Write(ctx context.Context, draft database.TaskEventDraft, immediate bool) error {
	writer.mu.Lock()
	if writer.err != nil {
		err := writer.err
		writer.mu.Unlock()

		return err
	}

	if writer.closed {
		writer.mu.Unlock()

		return ErrClosed
	}

	writer.pending = append(writer.pending, draft)
	flush := immediate || writer.options.FlushBatch > 0 && len(writer.pending) >= writer.options.FlushBatch
	writer.mu.Unlock()

	if flush {
		writer.flush(ctx)
	}

	return writer.loadErr()
}

// Close stops periodic flushing and makes one bounded, cancellation-independent
// attempt to commit remaining progress. It is idempotent.
func (writer *Writer) Close(ctx context.Context) error {
	writer.mu.Lock()
	if writer.closed {
		err := writer.err
		writer.mu.Unlock()

		return err
	}

	writer.closed = true
	writer.mu.Unlock()

	writer.Start(ctx)
	close(writer.stop)
	<-writer.done

	flushCtx := context.WithoutCancel(ctx)

	if writer.options.FinalizeTimeout > 0 {
		var cancel context.CancelFunc

		flushCtx, cancel = context.WithTimeout(flushCtx, writer.options.FinalizeTimeout)
		defer cancel()
	}

	writer.flush(flushCtx)

	return writer.loadErr()
}

func (writer *Writer) run(ctx context.Context) {
	defer close(writer.done)

	if writer.options.FlushInterval <= 0 {
		select {
		case <-writer.stop:
		case <-ctx.Done():
		}

		return
	}

	ticker := time.NewTicker(writer.options.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-writer.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			writer.flush(ctx)
		}
	}
}

func (writer *Writer) flush(ctx context.Context) {
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

	events, appended, err := writer.append(ctx, pending)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			writer.mu.Lock()
			writer.pending = append(pending, writer.pending...)
			writer.mu.Unlock()

			return
		}

		writer.recordErr(err)

		return
	}

	if !appended {
		writer.recordErr(ErrLeaseLost)

		return
	}

	for index := range events {
		if writer.publish != nil {
			writer.publish(&events[index])
		}
	}
}

func (writer *Writer) recordErr(err error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.err == nil {
		writer.err = err
	}
}

func (writer *Writer) loadErr() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	return writer.err
}
