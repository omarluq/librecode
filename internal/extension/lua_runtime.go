package extension

import "context"

// Name identifies the built-in Lua runtime adapter.
func (manager *Manager) Name() string {
	return "lua"
}

// Load loads one extension source into the Lua runtime adapter.
func (manager *Manager) Load(ctx context.Context, source RuntimeSource) error {
	return manager.LoadFile(ctx, source.Path)
}

// Close releases Lua runtime resources.
func (manager *Manager) Close() error {
	manager.Shutdown()

	return nil
}

var _ RuntimeAdapter = (*Manager)(nil)
