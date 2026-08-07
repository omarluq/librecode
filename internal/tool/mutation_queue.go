package tool

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

type fileMutationLocks struct {
	queues map[string][]*fileMutationReservation
	lock   sync.Mutex
}

type fileMutationReservation struct {
	locks *fileMutationLocks
	ready chan struct{}
	path  string
}

func newFileMutationLocks() *fileMutationLocks {
	return &fileMutationLocks{
		queues: map[string][]*fileMutationReservation{},
		lock:   sync.Mutex{},
	}
}

func (locks *fileMutationLocks) mutate(
	ctx context.Context,
	absolutePath string,
	mutate func() (Result, error),
) (Result, error) {
	return locks.reserve(absolutePath).execute(ctx, mutate)
}

func (locks *fileMutationLocks) reserve(absolutePath string) *fileMutationReservation {
	path := canonicalMutationPath(absolutePath)
	reservation := &fileMutationReservation{
		locks: locks,
		path:  path,
		ready: make(chan struct{}),
	}

	locks.lock.Lock()
	queue := locks.queues[path]

	locks.queues[path] = append(queue, reservation)
	if len(queue) == 0 {
		close(reservation.ready)
	}
	locks.lock.Unlock()

	return reservation
}

func (reservation *fileMutationReservation) execute(
	ctx context.Context,
	mutate func() (Result, error),
) (Result, error) {
	if err := ctx.Err(); err != nil {
		reservation.remove()

		return emptyToolResult(), err
	}

	select {
	case <-reservation.ready:
		defer reservation.remove()

		if err := ctx.Err(); err != nil {
			return emptyToolResult(), err
		}

		return mutate()
	case <-ctx.Done():
		reservation.remove()

		return emptyToolResult(), ctx.Err()
	}
}

func (reservation *fileMutationReservation) remove() {
	locks := reservation.locks
	locks.lock.Lock()
	defer locks.lock.Unlock()

	queue := locks.queues[reservation.path]
	index := slices.Index(queue, reservation)

	if index < 0 {
		return
	}

	wasFirst := index == 0

	queue = slices.Delete(queue, index, index+1)
	if len(queue) == 0 {
		delete(locks.queues, reservation.path)

		return
	}

	locks.queues[reservation.path] = queue
	if wasFirst {
		close(queue[0].ready)
	}
}

func canonicalMutationPath(absolutePath string) string {
	current := filepath.Clean(absolutePath)
	missing := []string{}

	for {
		canonicalPath, err := filepath.EvalSymlinks(current)
		if err == nil {
			for _, component := range slices.Backward(missing) {
				canonicalPath = filepath.Join(canonicalPath, component)
			}

			return canonicalPath
		}

		if !os.IsNotExist(err) {
			return filepath.Clean(absolutePath)
		}

		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(absolutePath)
		}

		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
