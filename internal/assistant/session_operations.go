package assistant

import (
	"context"
	"sync"

	"github.com/samber/oops"
)

// sessionOperationCoordinator serializes lineage-mutating operations per session.
// Operations for different sessions remain independent.
type sessionOperationCoordinator struct {
	slots map[string]*sessionOperationSlot
	mu    sync.Mutex
}

type sessionOperationSlot struct {
	token chan struct{}
	refs  int
}

func newSessionOperationCoordinator() *sessionOperationCoordinator {
	return &sessionOperationCoordinator{slots: make(map[string]*sessionOperationSlot), mu: sync.Mutex{}}
}

func (coordinator *sessionOperationCoordinator) acquire(
	ctx context.Context,
	sessionID string,
) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, oops.In("assistant").Code("session_operation_canceled").Wrapf(err, "acquire session operation")
	}

	coordinator.mu.Lock()

	slot := coordinator.slots[sessionID]
	if slot == nil {
		slot = &sessionOperationSlot{token: make(chan struct{}, 1), refs: 0}
		coordinator.slots[sessionID] = slot
	}

	slot.refs++
	coordinator.mu.Unlock()

	select {
	case slot.token <- struct{}{}:
		var once sync.Once

		return func() {
			once.Do(func() {
				<-slot.token
				coordinator.releaseReference(sessionID, slot)
			})
		}, nil
	case <-ctx.Done():
		coordinator.releaseReference(sessionID, slot)

		return nil, oops.In("assistant").Code("session_operation_canceled").Wrapf(
			ctx.Err(),
			"wait for session operation",
		)
	}
}

func (coordinator *sessionOperationCoordinator) releaseReference(
	sessionID string,
	slot *sessionOperationSlot,
) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()

	slot.refs--
	if slot.refs == 0 && coordinator.slots[sessionID] == slot {
		delete(coordinator.slots, sessionID)
	}
}
