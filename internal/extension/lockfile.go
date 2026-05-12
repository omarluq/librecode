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

// WriteLockFile writes an extension lockfile atomically enough for normal CLI use.
func WriteLockFile(path string, lockFile LockFile) error {
	if lockFile.Extensions == nil {
		lockFile.Extensions = map[string]LockedExtension{}
	}
	content, err := yaml.Marshal(lockFile)
	if err != nil {
		return fmt.Errorf("extension: marshal lockfile: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("extension: create lockfile dir: %w", err)
	}
	cleanPath := filepath.Clean(path)
	// #nosec G304 -- user-selected librecode lockfile path.
	if err := os.WriteFile(cleanPath, content, 0o600); err != nil {
		return fmt.Errorf("extension: write lockfile: %w", err)
	}

	return nil
}
