package extension

import "context"

// RuntimeSource describes one extension source handed to a runtime adapter.
type RuntimeSource struct {
	Path string
}

// RuntimeAdapter loads extension sources for one implementation language/runtime.
type RuntimeAdapter interface {
	Name() string
	Load(ctx context.Context, source RuntimeSource) error
	Close() error
}

// Host owns runtime adapters and exposes the extension manager surface to the app.
type Host struct {
	lua *Manager
}

// NewHost creates an extension host with the built-in Lua runtime adapter.
func NewHost(luaManager *Manager) *Host {
	return &Host{lua: luaManager}
}

// Lua returns the built-in Lua runtime adapter.
func (host *Host) Lua() *Manager {
	if host == nil {
		return nil
	}

	return host.lua
}
