package extension

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// LoadPaths discovers and loads Lua extension sources from files or directories.
func (manager *Manager) LoadPaths(ctx context.Context, paths []string) error {
	for _, extensionPath := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := manager.loadPath(ctx, extensionPath); err != nil {
			return err
		}
	}

	return nil
}

func (manager *Manager) loadPath(ctx context.Context, extensionPath string) error {
	sources, err := discoverLuaSources(extensionPath)
	if err != nil {
		return err
	}

	for _, source := range sources {
		if err := manager.loadSource(ctx, source); err != nil {
			return err
		}
	}

	return nil
}

func (manager *Manager) loadSource(ctx context.Context, source luaSource) error {
	if source.Manifest {
		return manager.LoadManifest(ctx, source.Path)
	}

	return manager.LoadFile(ctx, source.Path)
}

// LoadFile loads one Lua extension source file.
func (manager *Manager) LoadFile(ctx context.Context, extensionPath string) error {
	return manager.loadLuaFile(ctx, extensionPath, extensionName(extensionPath), extensionPath)
}

// LoadManifest loads one directory-based Lua extension manifest.
func (manager *Manager) LoadManifest(ctx context.Context, manifestPath string) error {
	manifest, err := manager.ReadManifest(manifestPath)
	if err != nil {
		return err
	}

	entry := strings.TrimSpace(manifest.Entry)
	if entry == "" {
		entry = "main.lua"
	}
	if filepath.IsAbs(entry) || strings.Contains(entry, "..") {
		return fmt.Errorf("extension: invalid entry %q", manifest.Entry)
	}

	entryPath := filepath.Join(filepath.Dir(manifestPath), entry)
	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		name = extensionName(filepath.Dir(manifestPath))
	}

	return manager.loadLuaFile(ctx, entryPath, name, filepath.Dir(manifestPath))
}

// ReadManifest reads a directory-based Lua manifest without executing extension entry code.
func (manager *Manager) ReadManifest(manifestPath string) (Manifest, error) {
	absolutePath, err := filepath.Abs(manifestPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("extension: resolve manifest: %w", err)
	}

	state := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer state.Close()
	openExtensionLibs(state)

	if err := state.DoFile(absolutePath); err != nil {
		return Manifest{}, fmt.Errorf("extension: load manifest %s: %w", absolutePath, err)
	}
	table, ok := state.Get(-1).(*lua.LTable)
	if !ok {
		return Manifest{}, fmt.Errorf("extension: manifest %s must return a table", absolutePath)
	}

	manifest := Manifest{
		Name:        luaTableString(table, "name", ""),
		Version:     luaTableString(table, "version", ""),
		APIVersion:  luaTableString(table, "api_version", ""),
		Description: luaTableString(table, "description", ""),
		Entry:       luaTableString(table, "entry", ""),
	}
	if strings.TrimSpace(manifest.Name) == "" {
		manifest.Name = extensionName(filepath.Dir(absolutePath))
	}

	return manifest, nil
}

func (manager *Manager) loadLuaFile(ctx context.Context, extensionPath, name, displayPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	absolutePath, err := filepath.Abs(extensionPath)
	if err != nil {
		return fmt.Errorf("extension: resolve path: %w", err)
	}

	manager.addModuleRootsForPath(absolutePath)
	extensionRuntime := &luaExtension{
		activeEvent:   nil,
		state:         lua.NewState(lua.Options{SkipOpenLibs: true}),
		name:          name,
		path:          displayPath,
		commands:      []string{},
		tools:         []string{},
		keymaps:       []string{},
		handlers:      []string{},
		lock:          sync.Mutex{},
		totalDuration: atomic.Int64{},
	}
	openExtensionLibs(extensionRuntime.state)
	manager.configurePackagePath(extensionRuntime.state)
	manager.installAPI(extensionRuntime)

	startedAt := time.Now()
	if err := extensionRuntime.state.DoFile(absolutePath); err != nil {
		extensionRuntime.state.Close()
		return fmt.Errorf("extension: load %s: %w", absolutePath, err)
	}
	if setupFn, ok := extensionRuntime.state.Get(-1).(*lua.LFunction); ok {
		extensionRuntime.state.Push(setupFn)
		extensionRuntime.state.Push(extensionRuntime.state.GetGlobal("librecode"))
		if err := extensionRuntime.state.PCall(1, 0, nil); err != nil {
			extensionRuntime.state.Close()
			return fmt.Errorf("extension: setup %s: %w", absolutePath, err)
		}
	}
	recordLuaCallDuration(extensionRuntime, startedAt)

	manager.lock.Lock()
	manager.extensions = append(manager.extensions, extensionRuntime)
	manager.lock.Unlock()
	manager.logger.Debug("loaded lua extension", slog.String("path", absolutePath))

	return nil
}

func (manager *Manager) installAPI(extensionRuntime *luaExtension) {
	apiTable := extensionRuntime.state.NewTable()
	extensionRuntime.state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"register_command": manager.luaRegisterCommand(extensionRuntime),
		"register_tool":    manager.luaRegisterTool(extensionRuntime),
		"on":               manager.luaOn(extensionRuntime),
		"log":              manager.luaLog(extensionRuntime),
	})
	extensionRuntime.state.SetField(apiTable, "api", manager.luaCoreAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "autocmd", manager.luaAutocmdAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "buf", manager.luaBufferAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "command", manager.luaCommandAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "event", manager.luaEventAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "action", manager.luaActionAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "timer", manager.luaTimerAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "keymap", manager.luaKeymapAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "layout", manager.luaLayoutAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "ui", manager.luaUIAPI(extensionRuntime))
	extensionRuntime.state.SetField(apiTable, "win", manager.luaWindowAPI(extensionRuntime))
	extensionRuntime.state.SetGlobal("librecode", apiTable)
	extensionRuntime.state.PreloadModule("librecode", func(state *lua.LState) int {
		state.Push(apiTable)

		return 1
	})
}

func (manager *Manager) luaRegisterCommand(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name, description, function := luaRegistrationArgs(state)
		definition := Command{Name: name, Description: description, Extension: extensionRuntime.name}

		manager.lock.Lock()
		manager.commands[name] = luaCommand{extension: extensionRuntime, function: function, definition: definition}
		extensionRuntime.commands = append(extensionRuntime.commands, name)
		manager.lock.Unlock()

		return 0
	}
}

func (manager *Manager) luaRegisterTool(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name, description, function := luaRegistrationArgs(state)
		definition := Tool{
			InputSchema: luaOptionalSchema(state, 4),
			Name:        name,
			Description: description,
			Extension:   extensionRuntime.name,
		}

		manager.lock.Lock()
		manager.tools[name] = luaTool{extension: extensionRuntime, function: function, definition: definition}
		extensionRuntime.tools = append(extensionRuntime.tools, name)
		manager.lock.Unlock()

		return 0
	}
}

func (manager *Manager) luaOn(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		eventName := state.CheckString(1)
		priority, function := luaEventHandlerArgs(state)
		manager.registerHandler(extensionRuntime, eventName, priority, function)

		return 0
	}
}

func (manager *Manager) luaLog(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		message := state.CheckString(1)
		manager.logger.Info(
			"lua extension",
			slog.String("extension", extensionRuntime.name),
			slog.String("message", message),
		)

		return 0
	}
}

func luaRegistrationArgs(state *lua.LState) (name, description string, function *lua.LFunction) {
	return state.CheckString(1), state.OptString(2, ""), state.CheckFunction(3)
}

func luaOptionalSchema(state *lua.LState, index int) map[string]any {
	if state.GetTop() < index {
		return map[string]any{}
	}
	if table, ok := state.Get(index).(*lua.LTable); ok {
		return luaTableToMap(table)
	}

	return map[string]any{}
}

func luaEventHandlerArgs(state *lua.LState) (priority int, function *lua.LFunction) {
	if handler, ok := state.Get(2).(*lua.LFunction); ok {
		return 0, handler
	}

	options := state.CheckTable(2)

	return int(lua.LVAsNumber(options.RawGetString("priority"))), state.CheckFunction(3)
}

func openExtensionLibs(state *lua.LState) {
	libraries := []struct {
		open lua.LGFunction
		name string
	}{
		{name: lua.BaseLibName, open: lua.OpenBase},
		{name: lua.LoadLibName, open: lua.OpenPackage},
		{name: lua.TabLibName, open: lua.OpenTable},
		{name: lua.StringLibName, open: lua.OpenString},
		{name: lua.MathLibName, open: lua.OpenMath},
		{name: lua.IoLibName, open: lua.OpenIo},
		{name: lua.OsLibName, open: lua.OpenOs},
		{name: lua.DebugLibName, open: lua.OpenDebug},
	}

	for _, library := range libraries {
		state.Push(state.NewFunction(library.open))
		state.Push(lua.LString(library.name))
		state.Call(1, 0)
	}
}

func extensionName(extensionPath string) string {
	baseName := filepath.Base(extensionPath)
	return strings.TrimSuffix(baseName, filepath.Ext(baseName))
}
