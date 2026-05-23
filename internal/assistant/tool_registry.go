package assistant

import (
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

type toolProvider interface {
	Tools() []extension.Tool
	tool.ExtensionToolRunner
}

func newToolRegistry(cwd string, provider toolProvider) (*tool.Registry, error) {
	registry := tool.NewRegistry(cwd)
	if provider == nil {
		return registry, nil
	}
	if err := registry.RegisterExtensions(provider, provider.Tools()); err != nil {
		return nil, oops.In("assistant").Code("register_extension_tools").Wrapf(err, "register extension tools")
	}

	return registry, nil
}
