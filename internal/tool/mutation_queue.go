package tool

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type fileMutationLocks struct {
	reservations []*fileMutationReservation
	lock         sync.Mutex
}

type fileMutationReservation struct {
	locks *fileMutationLocks
	ready chan struct{}
	path  string
}

func newFileMutationLocks() *fileMutationLocks {
	return &fileMutationLocks{reservations: []*fileMutationReservation{}, lock: sync.Mutex{}}
}

func (locks *fileMutationLocks) mutate(
	ctx context.Context,
	absolutePath string,
	mutate func() (Result, error),
) (Result, error) {
	return locks.reserve(absolutePath).execute(ctx, mutate)
}

func (locks *fileMutationLocks) reserve(absolutePath string) *fileMutationReservation {
	reservation, _ := locks.reserveMode(absolutePath, false)

	return reservation
}

func (locks *fileMutationLocks) tryReserve(absolutePath string) (*fileMutationReservation, bool) {
	return locks.reserveMode(absolutePath, true)
}

func (locks *fileMutationLocks) reserveMode(absolutePath string, nonblocking bool) (*fileMutationReservation, bool) {
	path := canonicalMutationPath(absolutePath)
	reservation := &fileMutationReservation{
		locks: locks,
		path:  path,
		ready: make(chan struct{}),
	}

	locks.lock.Lock()
	blocked := false

	for _, pending := range locks.reservations {
		if mutationPathsConflict(pending.path, path) {
			blocked = true

			break
		}
	}

	if blocked && nonblocking {
		locks.lock.Unlock()

		return nil, false
	}

	locks.reservations = append(locks.reservations, reservation)
	if !blocked {
		close(reservation.ready)
	}
	locks.lock.Unlock()

	return reservation, true
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

	index := slices.Index(locks.reservations, reservation)
	if index < 0 {
		return
	}

	locks.reservations = slices.Delete(locks.reservations, index, index+1)
	for candidateIndex, candidate := range locks.reservations {
		select {
		case <-candidate.ready:
			continue
		default:
		}

		blocked := false

		for priorIndex := range candidateIndex {
			if mutationPathsConflict(locks.reservations[priorIndex].path, candidate.path) {
				blocked = true

				break
			}
		}

		if !blocked {
			close(candidate.ready)
		}
	}
}

func mutationPathsConflict(left, right string) bool {
	return pathContains(left, right) || pathContains(right, left)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return parent == child
	}

	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
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

		if !errors.Is(err, fs.ErrNotExist) {
			// Preserve the original path when canonicalization fails for reasons
			// other than missing components, such as an intermediate non-directory.
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
