package di

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/auth"
	"github.com/omarluq/librecode/internal/core"
)

// AuthService owns provider credential storage.
type AuthService struct {
	Storage *auth.Storage
}

// NewAuthService wires librecode-style auth.json credential storage.
func NewAuthService(_ do.Injector) (*AuthService, error) {
	authPath, err := resolveAuthPath()
	if err != nil {
		return nil, err
	}
	storage, err := auth.NewStorage(context.Background(), auth.NewFileBackend(authPath))
	if err != nil {
		return nil, oops.In("auth").Code("load").Wrapf(err, "load auth storage")
	}

	return &AuthService{Storage: storage}, nil
}

func resolveAuthPath() (string, error) {
	projectPath, err := projectDataPath("auth.json")
	if err == nil {
		legacyProjectPath := filepath.Join(filepath.Dir(filepath.Dir(projectPath)), "auth.json")
		migrateErr := migrateLegacyFile(projectPath, legacyProjectPath)
		if migrateErr != nil {
			return "", oops.In("auth").Code("migrate_project").Wrapf(migrateErr, "migrate project auth storage")
		}
		if fileExists(projectPath) {
			return projectPath, nil
		}
	}

	globalPath, err := userDataPath("auth.json")
	if err != nil {
		return "", err
	}
	if err := migrateLegacyFile(globalPath, legacyUserConfigPath("auth.json")); err != nil {
		return "", oops.In("auth").Code("migrate").Wrapf(err, "migrate auth storage")
	}

	return globalPath, nil
}

func projectDataPath(filename string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", oops.In("config").Code("cwd").Wrapf(err, "resolve current directory")
	}

	return filepath.Join(core.ProjectConfigDir(cwd), filename), nil
}

func userDataPath(filename string) (string, error) {
	home, err := core.LibrecodeHome()
	if err != nil {
		return "", oops.In("config").Code("librecode_home").Wrapf(err, "resolve librecode home")
	}

	return filepath.Join(home, filename), nil
}

func legacyUserConfigPath(filename string) string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}

	return filepath.Join(configDir, "librecode", filename)
}

func migrateLegacyFile(newPath, legacyPath string) error {
	if legacyPath == "" || newPath == legacyPath || fileExists(newPath) || !fileExists(legacyPath) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
		return err
	}
	if err := os.Rename(legacyPath, newPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrPermission) && !errors.Is(err, os.ErrExist) {
		return err
	}

	// #nosec G304 -- migration path is an app-owned legacy path.
	content, err := os.ReadFile(filepath.Clean(legacyPath))
	if err != nil {
		return err
	}
	return writeMigratedFile(newPath, content)
}

func writeMigratedFile(path string, content []byte) error {
	//nolint:gosec // migration destination is an app-owned file with private mode.
	return os.WriteFile(filepath.Clean(path), content, 0o600)
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)

	return err == nil
}
