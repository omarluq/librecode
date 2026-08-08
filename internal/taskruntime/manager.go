package taskruntime

import (
	"context"
	"errors"
	"sync"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
)

// CancelFunc requests cancellation through a kind's specialized lifecycle service.
type CancelFunc func(context.Context, string, string) (*database.TaskEntity, bool, error)

// Manager provides transport-neutral lifecycle inspection while preserving typed cancellation behavior.
type Manager struct {
	tasks   *database.TaskRepository
	cancels map[string]CancelFunc
	mu      sync.RWMutex
}

// NewManager constructs generic lifecycle management over the authoritative tasks table.
func NewManager(tasks *database.TaskRepository) *Manager {
	return &Manager{tasks: tasks, cancels: make(map[string]CancelFunc), mu: sync.RWMutex{}}
}

// RegisterCancel installs the specialized cancellation path for a durable task kind.
func (manager *Manager) RegisterCancel(kind string, cancel CancelFunc) error {
	if kind == "" || cancel == nil {
		return errors.New("taskruntime: task kind and cancellation function are required")
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()

	if _, exists := manager.cancels[kind]; exists {
		return errors.New("taskruntime: cancellation function already registered")
	}

	manager.cancels[kind] = cancel

	return nil
}

// GetTask returns an owner-scoped generic lifecycle snapshot.
func (manager *Manager) GetTask(
	ctx context.Context, owner, taskID string,
) (*database.TaskEntity, bool, error) {
	task, found, err := manager.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, false, oops.In("taskruntime").Code("get_task").Wrapf(err, "get task")
	}

	if !found || task.OwnerSessionID != owner {
		return nil, false, nil
	}

	return task, true, nil
}

// ListTasks lists every supported durable task kind owned by a session.
func (manager *Manager) ListTasks(
	ctx context.Context, owner string, states []database.TaskState, limit int,
) ([]database.TaskEntity, error) {
	tasks, err := manager.tasks.ListOwned(ctx, owner, nil, states, limit)
	if err != nil {
		return nil, oops.In("taskruntime").Code("list_tasks").Wrapf(err, "list tasks")
	}

	return tasks, nil
}

// CancelTask dispatches cancellation to the task kind's specialized service.
func (manager *Manager) CancelTask(
	ctx context.Context, owner, taskID string,
) (*database.TaskEntity, bool, error) {
	task, found, err := manager.GetTask(ctx, owner, taskID)
	if err != nil || !found {
		return nil, found, err
	}

	manager.mu.RLock()
	cancel := manager.cancels[task.Kind]
	manager.mu.RUnlock()

	if cancel == nil {
		return nil, false, errors.New("taskruntime: task kind does not support generic cancellation")
	}

	return cancel(ctx, owner, taskID)
}
