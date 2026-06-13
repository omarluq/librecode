package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const (
	authFileMode                   = 0o600
	authDirMode                    = 0o700
	authCheckMemoryLockContextStep = "check memory auth lock context"
)

// FileBackend stores credentials in auth.json with process-local locking and atomic writes.
type FileBackend struct {
	path string
	lock sync.Mutex
}

// NewFileBackend creates a file-backed auth storage backend.
func NewFileBackend(path string) *FileBackend {
	return &FileBackend{path: filepath.Clean(path), lock: sync.Mutex{}}
}

// WithLock serializes access to auth.json for this process.
func (backend *FileBackend) WithLock(ctx context.Context, callback func(current []byte) (LockResult, error)) error {
	backend.lock.Lock()
	defer backend.lock.Unlock()

	if err := ctx.Err(); err != nil {
		return authError(err, "check auth lock context")
	}

	if err := ensureAuthFile(backend.path); err != nil {
		return err
	}

	current, err := readAuthFile(backend.path)
	if err != nil {
		return authError(err, authCheckMemoryLockContextStep)
	}

	result, err := callback(current)
	if err != nil {
		return err
	}

	if !result.Write {
		return authError(ctx.Err(), "check auth lock context")
	}

	if err := ctx.Err(); err != nil {
		return authError(err, "stat auth file")
	}

	return writeAuthFile(backend.path, result.Next)
}

// MemoryBackend stores auth JSON in process memory.
type MemoryBackend struct {
	value []byte
	lock  sync.Mutex
}

// NewMemoryBackend creates an in-memory backend from credentials.
func NewMemoryBackend(credentials map[string]Credential) (*MemoryBackend, error) {
	value, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return nil, authError(err, "encode memory auth")
	}

	return &MemoryBackend{value: value, lock: sync.Mutex{}}, nil
}

// WithLock serializes access to in-memory auth JSON.
func (backend *MemoryBackend) WithLock(ctx context.Context, callback func(current []byte) (LockResult, error)) error {
	backend.lock.Lock()
	defer backend.lock.Unlock()

	if err := ctx.Err(); err != nil {
		return authError(err, authCheckMemoryLockContextStep)
	}

	result, err := callback(append([]byte{}, backend.value...))
	if err != nil {
		return err
	}

	if result.Write {
		backend.value = append([]byte{}, result.Next...)
	}

	return authError(ctx.Err(), authCheckMemoryLockContextStep)
}

func ensureAuthFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, authDirMode); err != nil {
		return authError(err, "create auth directory")
	}

	if _, err := os.Stat(path); err == nil {
		return authError(os.Chmod(path, authFileMode), "chmod auth file")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return authError(err, "stat auth file")
	}

	return writeAuthFile(path, []byte("{}"))
}

func readAuthFile(path string) ([]byte, error) {
	cleanPath := filepath.Clean(path)

	content, err := fs.ReadFile(os.DirFS(filepath.Dir(cleanPath)), filepath.Base(cleanPath))

	return content, authError(err, "read auth file")
}

func writeAuthFile(path string, content []byte) error {
	cleanPath := filepath.Clean(path)
	dir := filepath.Dir(cleanPath)

	file, err := os.CreateTemp(dir, ".auth-*.json")
	if err != nil {
		return authError(err, "create temporary auth file")
	}

	tempPath := file.Name()

	writeErr := writeAndClose(file, content)
	if writeErr != nil {
		removeErr := os.Remove(tempPath)

		return errors.Join(writeErr, removeErr)
	}

	if err := os.Rename(tempPath, cleanPath); err != nil {
		removeErr := os.Remove(tempPath)

		return errors.Join(err, removeErr)
	}

	return authError(os.Chmod(cleanPath, authFileMode), "chmod auth file")
}

func writeAndClose(file *os.File, content []byte) error {
	_, writeErr := file.Write(content)
	chmodErr := file.Chmod(authFileMode)
	closeErr := file.Close()

	return errors.Join(writeErr, chmodErr, closeErr)
}
