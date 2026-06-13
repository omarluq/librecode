package tool

import "sync"

type fileMutationLocks struct {
	files map[string]*sync.Mutex
	lock  sync.Mutex
}

func newFileMutationLocks() *fileMutationLocks {
	return &fileMutationLocks{
		files: map[string]*sync.Mutex{},
		lock:  sync.Mutex{},
	}
}

func (locks *fileMutationLocks) mutate(absolutePath string, mutate func() (Result, error)) (Result, error) {
	fileLock := locks.lockFor(absolutePath)
	fileLock.Lock()
	defer fileLock.Unlock()

	return mutate()
}

func (locks *fileMutationLocks) lockFor(absolutePath string) *sync.Mutex {
	locks.lock.Lock()
	defer locks.lock.Unlock()

	fileLock, ok := locks.files[absolutePath]
	if ok {
		return fileLock
	}

	fileLock = &sync.Mutex{}
	locks.files[absolutePath] = fileLock

	return fileLock
}
