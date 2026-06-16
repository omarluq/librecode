package extension

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// LockFileName is the filename used for extension version locks.
	LockFileName = "extensions-lock.yaml"
)

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
