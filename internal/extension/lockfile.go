package extension

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LockFileName is the filename used for extension version locks.
const LockFileName = "extensions-lock.yaml"

// LockFile pins resolved extension versions.
type LockFile struct {
	Extensions map[string]LockedExtension `json:"extensions" yaml:"extensions"`
}

// LockedExtension pins one configured extension source.
type LockedExtension struct {
	Resolved string `json:"resolved,omitempty" yaml:"resolved,omitempty"`
	Version  string `json:"version,omitempty" yaml:"version,omitempty"`
}

// ReadLockFile loads an extension lockfile. Missing files return an empty lock.
func ReadLockFile(path string) (LockFile, error) {
	content, err := os.ReadFile(filepath.Clean(path)) // #nosec G304 -- user-selected librecode lockfile path.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return LockFile{Extensions: map[string]LockedExtension{}}, nil
		}
		return LockFile{}, fmt.Errorf("extension: read lockfile: %w", err)
	}

	lockFile := LockFile{Extensions: map[string]LockedExtension{}}
	if err := yaml.Unmarshal(content, &lockFile); err != nil {
		return LockFile{}, fmt.Errorf("extension: parse lockfile: %w", err)
	}
	if lockFile.Extensions == nil {
		lockFile.Extensions = map[string]LockedExtension{}
	}

	return lockFile, nil
}

// WriteLockFile writes an extension lockfile atomically.
func WriteLockFile(path string, lockFile LockFile) error {
	if lockFile.Extensions == nil {
		lockFile.Extensions = map[string]LockedExtension{}
	}
	content, err := yaml.Marshal(lockFile)
	if err != nil {
		return fmt.Errorf("extension: marshal lockfile: %w", err)
	}

	cleanPath := filepath.Clean(path)
	dir := filepath.Dir(cleanPath)
	if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
		return fmt.Errorf("extension: create lockfile dir: %w", mkdirErr)
	}

	tempPattern := "." + filepath.Base(cleanPath) + "-*.tmp"
	tempFile, err := os.CreateTemp(dir, tempPattern) // #nosec G304 -- user-selected librecode lockfile path.
	if err != nil {
		return fmt.Errorf("extension: create temporary lockfile: %w", err)
	}
	tempPath := tempFile.Name()
	removeTempFile := true
	defer func() {
		if removeTempFile {
			removeErr := os.Remove(tempPath)
			_ = removeErr
		}
	}()

	if _, err := tempFile.Write(content); err != nil {
		closeErr := tempFile.Close()
		_ = closeErr
		return fmt.Errorf("extension: write temporary lockfile: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		closeErr := tempFile.Close()
		_ = closeErr
		return fmt.Errorf("extension: chmod temporary lockfile: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("extension: close temporary lockfile: %w", err)
	}
	if err := os.Rename(tempPath, cleanPath); err != nil {
		return fmt.Errorf("extension: replace lockfile: %w", err)
	}
	removeTempFile = false

	return nil
}
