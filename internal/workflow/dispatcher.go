package workflow

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
)

const (
	defaultWorkerConcurrency = 2
	defaultDispatchInterval  = time.Second
	defaultDispatchBuffer    = 128
)

// DispatcherOptions configures durable workflow queue polling and execution.
type DispatcherOptions struct {
	Service     *Service
	Tasks       *database.TaskRepository
	Logger      *slog.Logger
	Concurrency int
	Buffer      int
	Interval    time.Duration
}

// Dispatcher polls durable queued workflows and executes atomically claimed runs.
type Dispatcher struct {
	service  *Service
	tasks    *database.TaskRepository
	logger   *slog.Logger
	queue    chan string
	cancel   context.CancelFunc
	done     <-chan struct{}
	wg       sync.WaitGroup
	interval time.Duration
}

// NewDispatcher starts durable workflow workers.
func NewDispatcher(ctx context.Context, options DispatcherOptions) (*Dispatcher, error) {
	if ctx == nil || options.Service == nil || options.Tasks == nil {
		return nil, oops.In("workflow").Code("invalid_dispatcher_dependencies").
			Errorf("process context, workflow service, and task repository are required")
	}

	concurrency := options.Concurrency
	if concurrency <= 0 {
		concurrency = defaultWorkerConcurrency
	}

	buffer := options.Buffer
	if buffer <= 0 {
		buffer = defaultDispatchBuffer
	}

	interval := options.Interval
	if interval <= 0 {
		interval = defaultDispatchInterval
	}

	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	dispatchCtx, cancel := context.WithCancel(ctx)
	dispatcher := &Dispatcher{
		service: options.Service, tasks: options.Tasks, logger: logger,
		queue: make(chan string, buffer), cancel: cancel, done: dispatchCtx.Done(),
		wg: sync.WaitGroup{}, interval: interval,
	}

	for range concurrency {
		dispatcher.wg.Add(1)
		go dispatcher.worker(dispatchCtx)
	}

	dispatcher.wg.Add(1)
	go dispatcher.poll(dispatchCtx)

	return dispatcher, nil
}

// Submit persists and schedules a workflow run.
func (dispatcher *Dispatcher) Submit(
	ctx context.Context,
	request *ServiceRequest,
) (*database.WorkflowRunEntity, error) {
	run, err := dispatcher.service.Submit(ctx, request)
	if err != nil {
		return nil, err
	}

	dispatcher.enqueue(run.Task.ID)

	return run, nil
}

// Await delegates durable completion waiting to the workflow service.
func (dispatcher *Dispatcher) Await(ctx context.Context, runID string) (*database.WorkflowRunEntity, error) {
	return dispatcher.service.Await(ctx, runID)
}

// Shutdown stops polling and waits for workers.
func (dispatcher *Dispatcher) Shutdown(ctx context.Context) error {
	dispatcher.cancel()

	done := make(chan struct{})

	go func() {
		dispatcher.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return oops.In("workflow").Code("dispatcher_shutdown").Wrapf(ctx.Err(), "shutdown workflow dispatcher")
	}
}

func (dispatcher *Dispatcher) poll(ctx context.Context) {
	defer dispatcher.wg.Done()

	dispatcher.enqueueQueued(ctx)

	ticker := time.NewTicker(dispatcher.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dispatcher.enqueueQueued(ctx)
		}
	}
}

func (dispatcher *Dispatcher) enqueueQueued(ctx context.Context) {
	tasks, err := dispatcher.tasks.ListByStates(
		ctx,
		database.TaskKindWorkflow,
		[]database.TaskState{database.TaskQueued},
		cap(dispatcher.queue),
	)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			dispatcher.logger.Error("list queued workflows", "error", err)
		}

		return
	}

	for index := range tasks {
		dispatcher.enqueue(tasks[index].ID)
	}
}

func (dispatcher *Dispatcher) enqueue(runID string) {
	select {
	case dispatcher.queue <- runID:
	case <-dispatcher.done:
	default:
	}
}

func (dispatcher *Dispatcher) worker(ctx context.Context) {
	defer dispatcher.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case runID := <-dispatcher.queue:
			if ctx.Err() != nil {
				return
			}

			if _, err := dispatcher.service.ExecuteQueued(ctx, runID); err != nil &&
				!errors.Is(err, context.Canceled) {
				dispatcher.logger.Error("execute queued workflow", "run_id", runID, "error", err)
			}
		}
	}
}
