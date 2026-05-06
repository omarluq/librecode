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
	authFileMode = 0o600
	authDirMode  = 0o700
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
		return err
	}
	if err := ensureAuthFile(backend.path); err != nil {
		return err
	}
	current, err := readAuthFile(backend.path)
	if err != nil {
		return err
	}
	result, err := callback(current)
	if err != nil {
		return err
	}
	if !result.Write {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
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
		return nil, err
	}

	return &MemoryBackend{value: value, lock: sync.Mutex{}}, nil
}

// WithLock serializes access to in-memory auth JSON.
func (backend *MemoryBackend) WithLock(ctx context.Context, callback func(current []byte) (LockResult, error)) error {
	backend.lock.Lock()
	defer backend.lock.Unlock()

	if err := ctx.Err(); err != nil {
		return err
	}
	result, err := callback(append([]byte{}, backend.value...))
	if err != nil {
		return err
	}
	if result.Write {
		backend.value = append([]byte{}, result.Next...)
	}

	return ctx.Err()
}

func ensureAuthFile(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, authDirMode); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return os.Chmod(path, authFileMode)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return writeAuthFile(path, []byte("{}"))
}

func readAuthFile(path string) ([]byte, error) {
	cleanPath := filepath.Clean(path)

	return fs.ReadFile(os.DirFS(filepath.Dir(cleanPath)), filepath.Base(cleanPath))
}

func writeAuthFile(path string, content []byte) error {
	cleanPath := filepath.Clean(path)
	dir := filepath.Dir(cleanPath)
	file, err := os.CreateTemp(dir, ".auth-*.json")
	if err != nil {
		return err
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

	return os.Chmod(cleanPath, authFileMode)
}

func writeAndClose(file *os.File, content []byte) error {
	_, writeErr := file.Write(content)
	chmodErr := file.Chmod(authFileMode)
	closeErr := file.Close()

	return errors.Join(writeErr, chmodErr, closeErr)
}
