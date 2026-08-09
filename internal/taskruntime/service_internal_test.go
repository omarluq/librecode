package taskruntime

import (
	"bytes"
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const (
	testTaskKind         = "test"
	testDoneSummary      = "done"
	testStartedEventKind = "task_started"
)

type ignoringHandler struct {
	started chan struct{}
	release <-chan struct{}
}

func (*ignoringHandler) Kind() string { return testTaskKind }
func (handler *ignoringHandler) Run(context.Context, *database.TaskEntity, EventSink) (Outcome, error) {
	close(handler.started)
	<-handler.release

	return Outcome{Value: nil, Summary: "late", ErrorCode: "", ErrorMessage: ""}, nil
}

type cancelAwareHandler struct {
	started  chan struct{}
	canceled chan struct{}
}

func (*cancelAwareHandler) Kind() string { return testTaskKind }
func (handler *cancelAwareHandler) Run(
	ctx context.Context, _ *database.TaskEntity, _ EventSink,
) (Outcome, error) {
	close(handler.started)
	<-ctx.Done()
	close(handler.canceled)

	return Outcome{}, oops.In("taskruntime").Code("handler_canceled").Wrapf(ctx.Err(), "wait for cancellation")
}

type panicHandler struct{}

func (*panicHandler) Kind() string { return "panic" }
func (*panicHandler) Run(context.Context, *database.TaskEntity, EventSink) (Outcome, error) {
	panic("boom")
}

type blockingHandler struct {
	release <-chan struct{}
	active  atomic.Int32
	maximum atomic.Int32
}

type admittingHandler struct {
	blocked map[string]bool
	started chan string
	calls   atomic.Int32
}

func (*admittingHandler) Kind() string { return testTaskKind }
func (handler *admittingHandler) TryAdmit(_ context.Context, task *database.TaskEntity) (bool, error) {
	handler.calls.Add(1)

	return !handler.blocked[task.ID], nil
}
func (*admittingHandler) ReleaseAdmission(string) {}
func (handler *admittingHandler) Run(
	_ context.Context, task *database.TaskEntity, _ EventSink,
) (Outcome, error) {
	handler.started <- task.ID

	return Outcome{Value: nil, Summary: testDoneSummary, ErrorCode: "", ErrorMessage: ""}, nil
}

func (*blockingHandler) Kind() string { return testTaskKind }
func (handler *blockingHandler) Run(context.Context, *database.TaskEntity, EventSink) (Outcome, error) {
	current := handler.active.Add(1)
	defer handler.active.Add(-1)

	for {
		maximum := handler.maximum.Load()
		if current <= maximum || handler.maximum.CompareAndSwap(maximum, current) {
			break
		}
	}

	<-handler.release

	return Outcome{
		Value: map[string]any{"done": true}, Summary: testDoneSummary, ErrorCode: "", ErrorMessage: "",
	}, nil
}

func TestBoundTextPreservesUTF8AndByteLimit(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "abc", boundText("abc", 0), "non-positive bounds preserve documented behavior")
	assert.Equal(t, "a", boundText("a€b", 3))
	assert.Equal(t, "a€", boundText("a€b", 4))
	assert.True(t, utf8.ValidString(boundText("a€b", 2)))
	assert.LessOrEqual(t, len(boundText("a€b", 2)), 2)
}

func TestServiceRecoversExpiredLeaseAtStartup(t *testing.T) {
	t.Parallel()

	owner, tasks := newRuntimeTestOwnerAndTasks(t, "recovery.db")
	created := createRuntimeTestTask(t, tasks, owner.ID, testTaskKind)
	claimed, err := tasks.ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: "expired-worker",
		LeaseExpiresAt: time.Now().Add(-time.Hour), EventKind: testStartedEventKind,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 1, PollInterval: time.Hour, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, RecoveryInterval: time.Hour,
		DefaultTimeout: 0, MaxPayloadBytes: 0,
	}, &blockingHandler{release: make(chan struct{}), active: atomic.Int32{}, maximum: atomic.Int32{}})
	require.NoError(t, err)
	require.NoError(t, service.Start(t.Context()))
	t.Cleanup(func() { require.NoError(t, service.Shutdown(context.Background())) })

	recovered, found, err := tasks.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskInterrupted, recovered.State)
	assert.Equal(t, "lease_expired", recovered.ErrorCode)
}

func TestServiceCancelActiveStopsHandlerAndSettlesCancellation(t *testing.T) {
	t.Parallel()

	owner, tasks := newRuntimeTestOwnerAndTasks(t, "cancel-active.db")
	created := createRuntimeTestTask(t, tasks, owner.ID, testTaskKind)

	handler := &cancelAwareHandler{started: make(chan struct{}), canceled: make(chan struct{})}
	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 1, PollInterval: time.Hour, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, RecoveryInterval: time.Hour,
		DefaultTimeout: 0, MaxPayloadBytes: 0,
	}, handler)
	require.NoError(t, err)
	require.NoError(t, service.Start(t.Context()))
	<-handler.started

	service.CancelActive("not-active")

	changed, err := tasks.Transition(
		t.Context(), created.ID, []database.TaskState{database.TaskRunning},
		database.TaskCanceling, "task_cancel_requested",
	)
	require.NoError(t, err)
	require.True(t, changed)
	service.CancelActive(created.ID)
	<-handler.canceled
	service.wg.Wait()

	finished, found, err := tasks.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskCanceled, finished.State)
	assert.Empty(t, finished.ErrorCode)
	require.NoError(t, service.Shutdown(context.Background()))
}

func TestServiceRecoversHandlerPanic(t *testing.T) {
	t.Parallel()

	connection := newRuntimeTestDatabase(t, "panic.db")
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)
	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 0, PollInterval: 0, LeaseDuration: 0,
		HeartbeatInterval: 0, RecoveryInterval: 0, DefaultTimeout: 0, MaxPayloadBytes: 0,
	}, &panicHandler{})
	require.NoError(t, err)

	task := &database.TaskEntity{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
		ID: "", Kind: "", ParentTaskID: "", OwnerSessionID: "", ConcurrencyKey: "", LeaseOwner: "",
		State: "", Result: "", ErrorCode: "", ErrorMessage: "",
	}
	outcome, err := service.safeRun(t.Context(), &panicHandler{}, task, nil)
	assert.Equal(t, Outcome{Value: nil, Summary: "", ErrorCode: "", ErrorMessage: ""}, outcome)
	require.EqualError(t, err, "task handler panicked: boom")
	coded, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "handler_panic", coded.Code())
}

func TestServiceHeartbeatLogsLostLeaseContextBeforeCancel(t *testing.T) {
	t.Parallel()

	connection := newRuntimeTestDatabase(t, "heartbeat-log.db")
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)

	var logs bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logs, nil))
	service, err := New(Options{
		Tasks: tasks, Logger: logger, Workers: 1, PollInterval: time.Hour,
		LeaseDuration: time.Second, HeartbeatInterval: time.Millisecond,
		RecoveryInterval: time.Hour, DefaultTimeout: time.Second, MaxPayloadBytes: 0,
	})
	require.NoError(t, err)

	heartbeatCtx, stopHeartbeat := context.WithCancel(t.Context())
	defer stopHeartbeat()

	canceled := make(chan struct{})
	done := make(chan struct{})

	go service.heartbeat(heartbeatCtx, func() {
		stopHeartbeat()
		close(canceled)
	}, "missing-task", done)

	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not cancel after losing the lease")
	}

	<-done

	assert.Contains(t, logs.String(), "renew task lease")
	assert.Contains(t, logs.String(), "task_id=missing-task")
	assert.Contains(t, logs.String(), "lease_owner="+service.leaseOwner)
	assert.Contains(t, logs.String(), "lease_lost")
}

func TestServiceLifecycleValidationAndIdempotence(t *testing.T) {
	t.Parallel()

	connection := newRuntimeTestDatabase(t, "lifecycle.db")
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)
	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 0, PollInterval: 0, LeaseDuration: 0,
		HeartbeatInterval: 0, RecoveryInterval: 0, DefaultTimeout: 0, MaxPayloadBytes: 0,
	}, &blockingHandler{release: make(chan struct{}), active: atomic.Int32{}, maximum: atomic.Int32{}})
	require.NoError(t, err)

	require.NoError(t, service.Shutdown(t.Context()))

	parent, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, service.Start(parent))
	require.NoError(t, service.Start(t.Context()))
	require.NoError(t, service.Shutdown(context.Background()))
	require.NoError(t, service.Shutdown(context.Background()))
}

func TestServiceShutdownDeadlineFencesLateHandlerSettlement(t *testing.T) {
	t.Parallel()

	owner, tasks := newRuntimeTestOwnerAndTasks(t, "shutdown.db")
	created := createRuntimeTestTask(t, tasks, owner.ID, testTaskKind)

	release := make(chan struct{})
	handler := &ignoringHandler{started: make(chan struct{}), release: release}
	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 1, PollInterval: time.Hour, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, RecoveryInterval: time.Hour,
		DefaultTimeout: time.Hour, MaxPayloadBytes: 0,
	}, handler)
	require.NoError(t, err)
	require.NoError(t, service.Start(t.Context()))
	<-handler.started

	shutdownCtx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, service.Shutdown(shutdownCtx), context.Canceled)
	close(release)

	require.Eventually(t, func() bool {
		service.mu.Lock()
		defer service.mu.Unlock()

		return len(service.active) == 0
	}, time.Second, time.Millisecond)
	loaded, found, err := tasks.Get(t.Context(), created.ID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, database.TaskRunning, loaded.State)
	assert.NotEmpty(t, loaded.LeaseOwner)
}

func TestServiceDurablyRejectsUnknownQueuedKinds(t *testing.T) {
	t.Parallel()

	owner, tasks := newRuntimeTestOwnerAndTasks(t, "unknown.db")
	created := createRuntimeTestTask(t, tasks, owner.ID, "unknown")

	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 1, PollInterval: time.Hour, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, RecoveryInterval: time.Hour,
		DefaultTimeout: time.Hour, MaxPayloadBytes: 0,
	}, &blockingHandler{release: make(chan struct{}), active: atomic.Int32{}, maximum: atomic.Int32{}})
	require.NoError(t, err)
	require.NoError(t, service.Start(t.Context()))
	require.Eventually(t, func() bool {
		loaded, found, loadErr := tasks.Get(t.Context(), created.ID)

		return loadErr == nil && found && loaded.State == database.TaskFailed && loaded.ErrorCode == "unknown_kind"
	}, time.Second, time.Millisecond)
	require.NoError(t, service.Shutdown(context.Background()))
}

func TestServiceDispatchSkipsBlockedTasksBeyondWorkerCount(t *testing.T) {
	t.Parallel()

	connection := newRuntimeTestDatabase(t, "fairness.db")
	sessions, err := database.NewSessionRepository(connection)
	require.NoError(t, err)
	owner, err := sessions.CreateSession(t.Context(), t.TempDir(), "owner", "")
	require.NoError(t, err)
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)

	created := make([]*database.TaskEntity, 3)
	for index := range created {
		created[index], err = tasks.Create(t.Context(), &database.TaskEntity{
			CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
			ID: "", Kind: testTaskKind, ParentTaskID: "", OwnerSessionID: owner.ID, ConcurrencyKey: "",
			LeaseOwner: "", State: "", Result: "", ErrorCode: "", ErrorMessage: "",
		})
		require.NoError(t, err)
	}

	handler := &admittingHandler{
		blocked: map[string]bool{created[0].ID: true, created[1].ID: true},
		started: make(chan string, 1), calls: atomic.Int32{},
	}
	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 2, PollInterval: time.Hour, LeaseDuration: time.Second,
		HeartbeatInterval: 100 * time.Millisecond, RecoveryInterval: time.Hour,
		DefaultTimeout: time.Second, MaxPayloadBytes: 0,
	}, handler)
	require.NoError(t, err)
	require.NoError(t, service.Start(t.Context()))

	select {
	case startedID := <-handler.started:
		assert.Equal(t, created[2].ID, startedID)
	case <-time.After(time.Second):
		t.Fatal("runnable task behind blocked tasks was not dispatched")
	}

	require.NoError(t, service.Shutdown(context.Background()))
}

func TestServiceDoesNotAdmitWhenWorkersAreFull(t *testing.T) {
	t.Parallel()

	connection := newRuntimeTestDatabase(t, "capacity.db")
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)

	handler := &admittingHandler{blocked: map[string]bool{}, started: make(chan string, 1), calls: atomic.Int32{}}
	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 1, PollInterval: 0, LeaseDuration: 0,
		HeartbeatInterval: 0, RecoveryInterval: 0, DefaultTimeout: 0, MaxPayloadBytes: 0,
	}, handler)
	require.NoError(t, err)

	service.sem <- struct{}{}

	full := service.dispatchTasks(t.Context(), testTaskKind, []database.TaskEntity{{
		CreatedAt: time.Time{}, StartedAt: nil, FinishedAt: nil, UpdatedAt: time.Time{}, LeaseExpiresAt: nil,
		ID: "queued", Kind: testTaskKind, ParentTaskID: "", OwnerSessionID: "", ConcurrencyKey: "",
		LeaseOwner: "", State: database.TaskQueued, Result: "", ErrorCode: "", ErrorMessage: "",
	}})

	assert.True(t, full)
	assert.Zero(t, handler.calls.Load())
}

func runtimeTestOptions(tasks *database.TaskRepository) Options {
	return Options{
		Tasks: tasks, Logger: nil, Workers: 0, PollInterval: 0, LeaseDuration: 0,
		HeartbeatInterval: 0, RecoveryInterval: 0, DefaultTimeout: 0, MaxPayloadBytes: 0,
	}
}

func TestServiceValidatesConfigurationAndHandlers(t *testing.T) {
	t.Parallel()

	connection := newRuntimeTestDatabase(t, "validation.db")
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)

	t.Run("missing repository", func(t *testing.T) {
		t.Parallel()

		service, newErr := New(runtimeTestOptions(nil))
		assert.Nil(t, service)
		require.EqualError(t, newErr, "task repository is required")
	})

	t.Run("heartbeat not shorter than lease", func(t *testing.T) {
		t.Parallel()

		options := runtimeTestOptions(tasks)
		options.LeaseDuration = time.Second
		options.HeartbeatInterval = time.Second
		service, newErr := New(options)
		assert.Nil(t, service)
		require.EqualError(t, newErr, "heartbeat must be shorter than lease duration")
	})

	t.Run("nil handler", func(t *testing.T) {
		t.Parallel()

		service, newErr := New(runtimeTestOptions(tasks), nil)
		assert.Nil(t, service)
		require.EqualError(t, newErr, "task handler and kind are required")
	})

	t.Run("duplicate handler", func(t *testing.T) {
		t.Parallel()

		valid := &panicHandler{}
		service, newErr := New(runtimeTestOptions(tasks), valid, valid)
		assert.Nil(t, service)
		require.EqualError(t, newErr, `duplicate handler "panic"`)
	})
}

func TestServiceEventSinkValidatesAndPersistsEvents(t *testing.T) {
	t.Parallel()

	owner, tasks := newRuntimeTestOwnerAndTasks(t, "event-sink.db")
	created := createRuntimeTestTask(t, tasks, owner.ID, testTaskKind)

	options := runtimeTestOptions(tasks)
	options.MaxPayloadBytes = 32
	service, err := New(options, &panicHandler{})
	require.NoError(t, err)
	claimed, err := tasks.ClaimQueued(t.Context(), &database.TaskClaim{
		TaskID: created.ID, LeaseOwner: service.leaseOwner,
		LeaseExpiresAt: time.Now().Add(time.Minute), EventKind: testStartedEventKind,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	sink := service.eventSink(created.ID)
	require.NoError(t, sink(t.Context(), "progress", map[string]any{"step": 1}))
	events, err := tasks.ListEvents(t.Context(), created.ID, 0, 10)
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, "progress", events[2].Event.Kind)
	assert.JSONEq(t, `{"step":1}`, events[2].Event.PayloadJSON)

	err = sink(t.Context(), "invalid", func() {})
	require.ErrorContains(t, err, "encode task event")
	err = sink(t.Context(), "large", string(make([]byte, 33)))
	require.EqualError(t, err, "task event payload exceeds 32 bytes")

	service.leaseOwner = "another-worker"
	err = sink(t.Context(), "lost", nil)
	require.ErrorContains(t, err, "task lease is no longer owned")

	service.abandon()
	require.ErrorIs(t, sink(t.Context(), "abandoned", nil), context.Canceled)
}

func TestServiceRequiresLifecycleContexts(t *testing.T) {
	t.Parallel()

	connection := newRuntimeTestDatabase(t, "contexts.db")
	tasks, err := database.NewTaskRepository(connection)
	require.NoError(t, err)
	service, err := New(runtimeTestOptions(tasks))
	require.NoError(t, err)

	var nilContext context.Context
	require.EqualError(t, service.Start(nilContext), "taskruntime: context is required")
	require.EqualError(t, service.Shutdown(nilContext), "taskruntime: shutdown context is required")
}

func TestServiceBoundsWorkersAndDrainsQueuedTasks(t *testing.T) {
	t.Parallel()

	owner, tasks := newRuntimeTestOwnerAndTasks(t, "runtime.db")
	for range 3 {
		createRuntimeTestTask(t, tasks, owner.ID, testTaskKind)
	}

	release := make(chan struct{})
	handler := &blockingHandler{release: release, active: atomic.Int32{}, maximum: atomic.Int32{}}
	service, err := New(Options{
		Tasks: tasks, Logger: nil, Workers: 2, PollInterval: 10 * time.Millisecond,
		LeaseDuration: 30 * time.Second, HeartbeatInterval: 5 * time.Second,
		RecoveryInterval: 30 * time.Second, DefaultTimeout: 30 * time.Second, MaxPayloadBytes: 0,
	}, handler)
	require.NoError(t, err)
	require.NoError(t, service.Start(t.Context()))
	require.Eventually(t, func() bool { return handler.active.Load() == 2 }, 10*time.Second, 50*time.Millisecond)
	assert.Equal(t, int32(2), handler.maximum.Load())
	queued, err := tasks.ListByStates(t.Context(), testTaskKind, []database.TaskState{database.TaskQueued}, 0)
	require.NoError(t, err)
	assert.Len(t, queued, 1)
	close(release)
	require.Eventually(t, func() bool {
		completed, queryErr := tasks.ListByStates(
			t.Context(), testTaskKind, []database.TaskState{database.TaskSucceeded}, 0,
		)

		return queryErr == nil && len(completed) == 3
	}, 10*time.Second, 50*time.Millisecond)
	require.NoError(t, service.Shutdown(context.Background()))
}
