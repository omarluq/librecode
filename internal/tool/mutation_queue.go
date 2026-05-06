package tool

import "sync"

var mutationLocks = struct {
	files map[string]*sync.Mutex
	lock  sync.Mutex
}{
	lock:  sync.Mutex{},
	files: map[string]*sync.Mutex{},
}

func withFileMutation[T any](absolutePath string, mutate func() (T, error)) (T, error) {
	fileLock := mutationLockFor(absolutePath)
	fileLock.Lock()
	defer fileLock.Unlock()

	return mutate()
}

func mutationLockFor(absolutePath string) *sync.Mutex {
	mutationLocks.lock.Lock()
	defer mutationLocks.lock.Unlock()

	fileLock, ok := mutationLocks.files[absolutePath]
	if ok {
		return fileLock
	}
	fileLock = &sync.Mutex{}
	mutationLocks.files[absolutePath] = fileLock

	return fileLock
}
