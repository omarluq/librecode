package di

import (
	"path/filepath"

	"github.com/samber/do/v2"
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/tool"
)

// ToolService exposes Pi-style built-in coding tools for the process working directory.
type ToolService struct {
	Registry *tool.Registry
}

// NewToolService wires the built-in tool registry.
func NewToolService(_ do.Injector) (*ToolService, error) {
	cwd, err := filepath.Abs(".")
	if err != nil {
		return nil, oops.In("tool").Code("resolve_cwd").Wrapf(err, "resolve tool working directory")
	}

	return &ToolService{Registry: tool.NewRegistry(cwd)}, nil
}
