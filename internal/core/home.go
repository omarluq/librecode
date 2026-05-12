package core

import (
	"os"
	"path/filepath"
	"strings"
)

const librecodeHomeEnv = "LIBRECODE_HOME"

// LibrecodeHome returns the user-level librecode home directory.
//
// LIBRECODE_HOME overrides the default ~/.librecode location. The returned path
// is cleaned and may be relative only when the caller explicitly configured a
// relative LIBRECODE_HOME.
func LibrecodeHome() (string, error) {
	if home := strings.TrimSpace(os.Getenv(librecodeHomeEnv)); home != "" {
		return filepath.Clean(normalizeResourcePath(home)), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ConfigDirName), nil
}

// ProjectConfigDir returns the project-local librecode directory for cwd.
func ProjectConfigDir(cwd string) string {
	return filepath.Join(cwd, ConfigDirName)
}
