package assistant

import (
	"reflect"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tool"
)

var nilableToolProviderKinds = map[reflect.Kind]struct{}{
	reflect.Chan:      {},
	reflect.Func:      {},
	reflect.Interface: {},
	reflect.Map:       {},
	reflect.Pointer:   {},
	reflect.Slice:     {},
}

type toolProvider interface {
	Tools() []extension.Tool
	tool.ExtensionToolRunner
}

func newToolRegistry(cwd string, provider toolProvider) (*tool.Registry, error) {
	registry := tool.NewRegistry(cwd)
	if isNilToolProvider(provider) {
		return registry, nil
	}
	if err := registry.RegisterExtensions(provider, provider.Tools()); err != nil {
		return nil, oops.In("assistant").Code("register_extension_tools").Wrapf(err, "register extension tools")
	}

	return registry, nil
}

func isNilToolProvider(provider toolProvider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	if !value.IsValid() {
		return true
	}

	if _, ok := nilableToolProviderKinds[value.Kind()]; !ok {
		return false
	}

	return value.IsNil()
}
