package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

type luaPlugin struct {
	name     string
	path     string
	state    *lua.LState
	lock     sync.Mutex
	commands []string
	tools    []string
}

type luaCommand struct {
	definition Command
	plugin     *luaPlugin
	function   *lua.LFunction
}

type luaTool struct {
	definition Tool
	plugin     *luaPlugin
	function   *lua.LFunction
}

type luaEventHandler struct {
	plugin   *luaPlugin
	function *lua.LFunction
}

// Manager owns Lua plugin runtimes and registered commands/tools.
type Manager struct {
	logger   *slog.Logger
	lock     sync.RWMutex
	plugins  []*luaPlugin
	commands map[string]luaCommand
	tools    map[string]luaTool
	handlers map[string][]luaEventHandler
}

// NewManager creates an empty Lua plugin manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		logger:   logger,
		lock:     sync.RWMutex{},
		plugins:  []*luaPlugin{},
		commands: map[string]luaCommand{},
		tools:    map[string]luaTool{},
		handlers: map[string][]luaEventHandler{},
	}
}

// LoadPaths discovers and loads Lua plugins from files or directories.
func (manager *Manager) LoadPaths(ctx context.Context, paths []string) error {
	for _, pluginPath := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}

		files, err := discoverLuaFiles(pluginPath)
		if err != nil {
			return err
		}

		for _, file := range files {
			if err := manager.LoadFile(ctx, file); err != nil {
				return err
			}
		}
	}

	return nil
}

// LoadFile loads one Lua plugin file.
func (manager *Manager) LoadFile(ctx context.Context, pluginPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	absolutePath, err := filepath.Abs(pluginPath)
	if err != nil {
		return fmt.Errorf("plugin: resolve path: %w", err)
	}

	pluginRuntime := &luaPlugin{
		name:     pluginName(absolutePath),
		path:     absolutePath,
		state:    lua.NewState(lua.Options{SkipOpenLibs: true}),
		lock:     sync.Mutex{},
		commands: []string{},
		tools:    []string{},
	}
	openSafeLibs(pluginRuntime.state)
	manager.installAPI(pluginRuntime)

	// Plugins are explicitly configured executable code, so loading a dynamic path is intentional.
	if err := pluginRuntime.state.DoFile(absolutePath); err != nil { //nolint:gosec // Lua plugins are trusted user code.
		pluginRuntime.state.Close()
		return fmt.Errorf("plugin: load %s: %w", absolutePath, err)
	}

	manager.lock.Lock()
	manager.plugins = append(manager.plugins, pluginRuntime)
	manager.lock.Unlock()
	manager.logger.Debug("loaded lua plugin", slog.String("path", absolutePath))

	return nil
}

// Plugins returns loaded plugin metadata.
func (manager *Manager) Plugins() []LoadedPlugin {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	plugins := make([]LoadedPlugin, 0, len(manager.plugins))
	for _, pluginRuntime := range manager.plugins {
		plugins = append(plugins, LoadedPlugin{
			Name:     pluginRuntime.name,
			Path:     pluginRuntime.path,
			Commands: append([]string{}, pluginRuntime.commands...),
			Tools:    append([]string{}, pluginRuntime.tools...),
		})
	}

	return plugins
}

// Commands returns registered Lua commands sorted by name.
func (manager *Manager) Commands() []Command {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	commands := make([]Command, 0, len(manager.commands))
	for _, command := range manager.commands {
		commands = append(commands, command.definition)
	}
	sort.Slice(commands, func(leftIndex int, rightIndex int) bool {
		return commands[leftIndex].Name < commands[rightIndex].Name
	})

	return commands
}

// Tools returns registered Lua tools sorted by name.
func (manager *Manager) Tools() []Tool {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	tools := make([]Tool, 0, len(manager.tools))
	for _, tool := range manager.tools {
		tools = append(tools, tool.definition)
	}
	sort.Slice(tools, func(leftIndex int, rightIndex int) bool {
		return tools[leftIndex].Name < tools[rightIndex].Name
	})

	return tools
}

// ExecuteCommand runs a registered Lua slash command.
func (manager *Manager) ExecuteCommand(ctx context.Context, name string, args string) (string, error) {
	manager.lock.RLock()
	command, ok := manager.commands[name]
	manager.lock.RUnlock()
	if !ok {
		return "", fmt.Errorf("plugin: command %q not found", name)
	}

	result, err := callLua(command.plugin, command.function, lua.LString(args))
	if err != nil {
		return "", fmt.Errorf("plugin: command %q failed: %w", name, err)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return result.String(), nil
}

// ExecuteTool runs a registered Lua tool.
func (manager *Manager) ExecuteTool(ctx context.Context, name string, args map[string]any) (ToolResult, error) {
	manager.lock.RLock()
	tool, ok := manager.tools[name]
	manager.lock.RUnlock()
	if !ok {
		return ToolResult{Content: "", Details: map[string]any{}}, fmt.Errorf("plugin: tool %q not found", name)
	}

	argumentTable := mapToLuaTable(tool.plugin.state, args)
	result, err := callLua(tool.plugin, tool.function, argumentTable)
	if err != nil {
		return ToolResult{Content: "", Details: map[string]any{}}, fmt.Errorf("plugin: tool %q failed: %w", name, err)
	}
	if err := ctx.Err(); err != nil {
		return ToolResult{Content: "", Details: map[string]any{}}, err
	}

	return luaToolResult(result), nil
}

// Emit sends an event to registered Lua handlers.
func (manager *Manager) Emit(ctx context.Context, eventName string, payload map[string]any) error {
	manager.lock.RLock()
	handlers := append([]luaEventHandler{}, manager.handlers[eventName]...)
	manager.lock.RUnlock()

	for _, handler := range handlers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := callLua(handler.plugin, handler.function, lua.LString(eventName), mapToLuaTable(handler.plugin.state, payload)); err != nil {
			return fmt.Errorf("plugin: event %q failed: %w", eventName, err)
		}
	}

	return nil
}

// Shutdown closes all Lua runtimes.
func (manager *Manager) Shutdown() {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	for _, pluginRuntime := range manager.plugins {
		pluginRuntime.state.Close()
	}
	manager.plugins = []*luaPlugin{}
	manager.commands = map[string]luaCommand{}
	manager.tools = map[string]luaTool{}
	manager.handlers = map[string][]luaEventHandler{}
}

func (manager *Manager) installAPI(pluginRuntime *luaPlugin) {
	apiTable := pluginRuntime.state.NewTable()
	pluginRuntime.state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"register_command": manager.luaRegisterCommand(pluginRuntime),
		"register_tool":    manager.luaRegisterTool(pluginRuntime),
		"on":               manager.luaOn(pluginRuntime),
		"log":              manager.luaLog(pluginRuntime),
	})
	pluginRuntime.state.SetGlobal("librecode", apiTable)
}

func (manager *Manager) luaRegisterCommand(pluginRuntime *luaPlugin) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		description := state.OptString(2, "")
		function := state.CheckFunction(3)
		definition := Command{Name: name, Description: description, Plugin: pluginRuntime.name}

		manager.lock.Lock()
		manager.commands[name] = luaCommand{definition: definition, plugin: pluginRuntime, function: function}
		pluginRuntime.commands = append(pluginRuntime.commands, name)
		manager.lock.Unlock()

		return 0
	}
}

func (manager *Manager) luaRegisterTool(pluginRuntime *luaPlugin) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		description := state.OptString(2, "")
		function := state.CheckFunction(3)
		definition := Tool{Name: name, Description: description, Plugin: pluginRuntime.name}

		manager.lock.Lock()
		manager.tools[name] = luaTool{definition: definition, plugin: pluginRuntime, function: function}
		pluginRuntime.tools = append(pluginRuntime.tools, name)
		manager.lock.Unlock()

		return 0
	}
}

func (manager *Manager) luaOn(pluginRuntime *luaPlugin) lua.LGFunction {
	return func(state *lua.LState) int {
		eventName := state.CheckString(1)
		function := state.CheckFunction(2)

		manager.lock.Lock()
		manager.handlers[eventName] = append(manager.handlers[eventName], luaEventHandler{
			plugin:   pluginRuntime,
			function: function,
		})
		manager.lock.Unlock()

		return 0
	}
}

func (manager *Manager) luaLog(pluginRuntime *luaPlugin) lua.LGFunction {
	return func(state *lua.LState) int {
		message := state.CheckString(1)
		manager.logger.Info("lua plugin", slog.String("plugin", pluginRuntime.name), slog.String("message", message))

		return 0
	}
}

func callLua(pluginRuntime *luaPlugin, function *lua.LFunction, args ...lua.LValue) (lua.LValue, error) {
	pluginRuntime.lock.Lock()
	defer pluginRuntime.lock.Unlock()

	top := pluginRuntime.state.GetTop()
	defer pluginRuntime.state.SetTop(top)

	if err := pluginRuntime.state.CallByParam(lua.P{Fn: function, NRet: 1, Protect: true}, args...); err != nil {
		return lua.LNil, err
	}

	return pluginRuntime.state.Get(-1), nil
}

func discoverLuaFiles(pluginPath string) ([]string, error) {
	if pluginPath == "" {
		return []string{}, nil
	}

	info, err := os.Stat(pluginPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("plugin: stat %s: %w", pluginPath, err)
	}

	if !info.IsDir() {
		if strings.HasSuffix(pluginPath, ".lua") {
			return []string{pluginPath}, nil
		}
		return []string{}, nil
	}

	return walkLuaDir(pluginPath)
}

func walkLuaDir(root string) ([]string, error) {
	files := []string{}
	walkErr := filepath.WalkDir(root, func(currentPath string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if dirEntry.IsDir() || !strings.HasSuffix(currentPath, ".lua") {
			return nil
		}
		files = append(files, currentPath)

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("plugin: walk %s: %w", root, walkErr)
	}
	sort.Strings(files)

	return files, nil
}

func openSafeLibs(state *lua.LState) {
	for _, library := range []struct {
		name string
		open lua.LGFunction
	}{
		{name: lua.BaseLibName, open: lua.OpenBase},
		{name: lua.TabLibName, open: lua.OpenTable},
		{name: lua.StringLibName, open: lua.OpenString},
		{name: lua.MathLibName, open: lua.OpenMath},
	} {
		state.Push(state.NewFunction(library.open))
		state.Push(lua.LString(library.name))
		state.Call(1, 0)
	}
}

func pluginName(pluginPath string) string {
	baseName := filepath.Base(pluginPath)
	return strings.TrimSuffix(baseName, filepath.Ext(baseName))
}
