package di

import (
	"context"
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
	if err == nil && fileExists(projectPath) {
		return projectPath, nil
	}

	globalPath, err := userDataPath("auth.json")
	if err != nil {
		return "", err
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

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)

	return err == nil
}
