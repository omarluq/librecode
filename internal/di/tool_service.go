package di

import (
	"path/filepath"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/tool"
)

// ToolService exposes librecode-style built-in coding tools for the process working directory.
type ToolService struct {
	Registry    *tool.Registry
	Coordinator *tool.Coordinator
}

// NewToolService wires the built-in tool registry.
func NewToolService(_ do.Injector) (*ToolService, error) {
	cwd, err := filepath.Abs(".")
	if err != nil {
		return nil, oops.In("tool").Code("resolve_cwd").Wrapf(err, "resolve tool working directory")
	}

	coordinator := tool.NewCoordinator()

	names := []tool.Name{
		tool.NameRead,
		tool.NameBash,
		tool.NameEdit,
		tool.NameWrite,
		tool.NameGrep,
		tool.NameFind,
		tool.NameLS,
		tool.NameAST,
		tool.NameFetch,
	}

	registry, err := tool.NewRegistryWithCoordinator(cwd, names, coordinator)
	if err != nil {
		return nil, oops.In("tool").Code("create_registry").Wrapf(err, "create tool registry")
	}

	return &ToolService{Registry: registry, Coordinator: coordinator}, nil
}
