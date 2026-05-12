package di

import (
	"context"
	"os"
	"path/filepath"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/auth"
)

const librecodeConfigDir = "librecode"

// AuthService owns provider credential storage.
type AuthService struct {
	Storage *auth.Storage
}

// NewAuthService wires librecode-style auth.json credential storage.
func NewAuthService(_ do.Injector) (*AuthService, error) {
	authPath, err := userConfigPath("auth.json")
	if err != nil {
		return nil, err
	}
	storage, err := auth.NewStorage(context.Background(), auth.NewFileBackend(authPath))
	if err != nil {
		return nil, oops.In("auth").Code("load").Wrapf(err, "load auth storage")
	}

	return &AuthService{Storage: storage}, nil
}

func userConfigPath(filename string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", oops.In("config").Code("user_config_dir").Wrapf(err, "resolve user config dir")
	}

	return filepath.Join(configDir, librecodeConfigDir, filename), nil
}
