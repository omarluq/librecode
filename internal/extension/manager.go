package extension

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

type luaExtension struct {
	state         *lua.LState
	name          string
	path          string
	commands      []string
	tools         []string
	composerModes []string
	lock          sync.Mutex
}

type luaCommand struct {
	extension  *luaExtension
	function   *lua.LFunction
	definition Command
}

type luaTool struct {
	extension  *luaExtension
	function   *lua.LFunction
	definition Tool
}

type luaHookHandler struct {
	extension *luaExtension
	function  *lua.LFunction
}

type luaComposerMode struct {
	extension  *luaExtension
	function   *lua.LFunction
	definition ComposerMode
}

// Manager owns Lua extension runtimes and registered commands/tools.
type Manager struct {
	logger        *slog.Logger
	commands      map[string]luaCommand
	tools         map[string]luaTool
	composerModes map[string]luaComposerMode
	handlers      map[string][]luaHookHandler
	extensions    []*luaExtension
	lock          sync.RWMutex
}

// NewManager creates an empty Lua extension manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		logger:        logger,
		commands:      map[string]luaCommand{},
		tools:         map[string]luaTool{},
		composerModes: map[string]luaComposerMode{},
		handlers:      map[string][]luaHookHandler{},
		extensions:    []*luaExtension{},
		lock:          sync.RWMutex{},
	}
}

// LoadPaths discovers and loads Lua extensions from files or directories.
func (manager *Manager) LoadPaths(ctx context.Context, paths []string) error {
	for _, extensionPath := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}

		files, err := discoverLuaFiles(extensionPath)
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

// LoadFile loads one Lua extension file.
func (manager *Manager) LoadFile(ctx context.Context, extensionPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	absolutePath, err := filepath.Abs(extensionPath)
	if err != nil {
		return fmt.Errorf("extension: resolve path: %w", err)
	}

	extensionRuntime := &luaExtension{
		state:         lua.NewState(lua.Options{SkipOpenLibs: true}),
		name:          extensionName(absolutePath),
		path:          absolutePath,
		commands:      []string{},
		tools:         []string{},
		composerModes: []string{},
		lock:          sync.Mutex{},
	}
	openSafeLibs(extensionRuntime.state)
	manager.installAPI(extensionRuntime)

	if err := extensionRuntime.state.DoFile(absolutePath); err != nil {
		extensionRuntime.state.Close()
		return fmt.Errorf("extension: load %s: %w", absolutePath, err)
	}

	manager.lock.Lock()
	manager.extensions = append(manager.extensions, extensionRuntime)
	manager.lock.Unlock()
	manager.logger.Debug("loaded lua extension", slog.String("path", absolutePath))

	return nil
}

// Extensions returns loaded extension metadata.
func (manager *Manager) Extensions() []LoadedExtension {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	extensions := make([]LoadedExtension, 0, len(manager.extensions))
	for _, extensionRuntime := range manager.extensions {
		extensions = append(extensions, LoadedExtension{
			Name:          extensionRuntime.name,
			Path:          extensionRuntime.path,
			Commands:      append([]string{}, extensionRuntime.commands...),
			Tools:         append([]string{}, extensionRuntime.tools...),
			ComposerModes: append([]string{}, extensionRuntime.composerModes...),
		})
	}

	return extensions
}

// Commands returns registered Lua commands sorted by name.
func (manager *Manager) Commands() []Command {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	commands := make([]Command, 0, len(manager.commands))
	for _, command := range manager.commands {
		commands = append(commands, command.definition)
	}
	sort.Slice(commands, func(leftIndex, rightIndex int) bool {
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
	sort.Slice(tools, func(leftIndex, rightIndex int) bool {
		return tools[leftIndex].Name < tools[rightIndex].Name
	})

	return tools
}

// ComposerModes returns registered terminal composer modes sorted by name.
func (manager *Manager) ComposerModes() []ComposerMode {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	modes := make([]ComposerMode, 0, len(manager.composerModes))
	for _, mode := range manager.composerModes {
		modes = append(modes, mode.definition)
	}
	sort.Slice(modes, func(leftIndex, rightIndex int) bool {
		return modes[leftIndex].Name < modes[rightIndex].Name
	})

	return modes
}

// ExecuteCommand runs a registered Lua slash command.
func (manager *Manager) ExecuteCommand(ctx context.Context, name, args string) (string, error) {
	manager.lock.RLock()
	command, ok := manager.commands[name]
	manager.lock.RUnlock()
	if !ok {
		return "", fmt.Errorf("extension: command %q not found", name)
	}

	result, err := callLua(command.extension, command.function, lua.LString(args))
	if err != nil {
		return "", fmt.Errorf("extension: command %q failed: %w", name, err)
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
		return ToolResult{Details: map[string]any{}, Content: ""}, fmt.Errorf("extension: tool %q not found", name)
	}

	argumentTable := mapToLuaTable(tool.extension.state, args)
	result, err := callLua(tool.extension, tool.function, argumentTable)
	if err != nil {
		return ToolResult{Details: map[string]any{}, Content: ""},
			fmt.Errorf("extension: tool %q failed: %w", name, err)
	}
	if err := ctx.Err(); err != nil {
		return ToolResult{Details: map[string]any{}, Content: ""}, err
	}

	return luaToolResult(result), nil
}

// HandleComposerKey runs a registered Lua composer mode key handler.
func (manager *Manager) HandleComposerKey(
	ctx context.Context,
	mode string,
	event ComposerKeyEvent,
	state ComposerState,
) (ComposerResult, error) {
	manager.lock.RLock()
	composerMode, ok := manager.composerModes[mode]
	manager.lock.RUnlock()
	if !ok || composerMode.function == nil {
		return emptyComposerResult(), nil
	}

	luaEvent := composerEventTable(composerMode.extension.state, event)
	luaState := composerStateTable(composerMode.extension.state, state)
	result, err := callLua(composerMode.extension, composerMode.function, luaEvent, luaState)
	if err != nil {
		return emptyComposerResult(), fmt.Errorf("extension: composer mode %q failed: %w", mode, err)
	}
	if err := ctx.Err(); err != nil {
		return emptyComposerResult(), err
	}

	return luaComposerResult(result), nil
}

// Emit sends an event to registered Lua handlers.
func (manager *Manager) Emit(ctx context.Context, eventName string, payload map[string]any) error {
	manager.lock.RLock()
	handlers := append([]luaHookHandler{}, manager.handlers[eventName]...)
	manager.lock.RUnlock()

	for _, handler := range handlers {
		if err := ctx.Err(); err != nil {
			return err
		}

		payloadTable := mapToLuaTable(handler.extension.state, payload)
		_, err := callLua(handler.extension, handler.function, lua.LString(eventName), payloadTable)
		if err != nil {
			return fmt.Errorf("extension: event %q failed: %w", eventName, err)
		}
	}

	return nil
}

// Shutdown closes all Lua runtimes.
func (manager *Manager) Shutdown() {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	for _, extensionRuntime := range manager.extensions {
		extensionRuntime.state.Close()
	}
	manager.extensions = []*luaExtension{}
	manager.commands = map[string]luaCommand{}
	manager.tools = map[string]luaTool{}
	manager.composerModes = map[string]luaComposerMode{}
	manager.handlers = map[string][]luaHookHandler{}
}

func (manager *Manager) installAPI(extensionRuntime *luaExtension) {
	apiTable := extensionRuntime.state.NewTable()
	extensionRuntime.state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"register_command":       manager.luaRegisterCommand(extensionRuntime),
		"register_tool":          manager.luaRegisterTool(extensionRuntime),
		"register_composer_mode": manager.luaRegisterComposerMode(extensionRuntime),
		"on":                     manager.luaOn(extensionRuntime),
		"log":                    manager.luaLog(extensionRuntime),
	})
	extensionRuntime.state.SetGlobal("librecode", apiTable)
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
		definition := Tool{Name: name, Description: description, Extension: extensionRuntime.name}

		manager.lock.Lock()
		manager.tools[name] = luaTool{extension: extensionRuntime, function: function, definition: definition}
		extensionRuntime.tools = append(extensionRuntime.tools, name)
		manager.lock.Unlock()

		return 0
	}
}

func (manager *Manager) luaRegisterComposerMode(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		description := state.OptString(2, "")
		options := state.OptTable(3, state.NewTable())
		definition := ComposerMode{
			Name:        name,
			Description: description,
			Extension:   extensionRuntime.name,
			Label:       luaTableString(options, "label", ""),
			Default:     luaTableBool(options, "default", false),
		}

		manager.lock.Lock()
		manager.composerModes[name] = luaComposerMode{
			extension:  extensionRuntime,
			function:   luaTableFunction(options, "on_key"),
			definition: definition,
		}
		extensionRuntime.composerModes = append(extensionRuntime.composerModes, name)
		manager.lock.Unlock()

		return 0
	}
}

func (manager *Manager) luaOn(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		eventName := state.CheckString(1)
		function := state.CheckFunction(2)

		manager.lock.Lock()
		manager.handlers[eventName] = append(manager.handlers[eventName], luaHookHandler{
			extension: extensionRuntime,
			function:  function,
		})
		manager.lock.Unlock()

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

func callLua(extensionRuntime *luaExtension, function *lua.LFunction, args ...lua.LValue) (lua.LValue, error) {
	extensionRuntime.lock.Lock()
	defer extensionRuntime.lock.Unlock()

	top := extensionRuntime.state.GetTop()
	defer extensionRuntime.state.SetTop(top)

	if err := extensionRuntime.state.CallByParam(lua.P{Fn: function, NRet: 1, Protect: true}, args...); err != nil {
		return lua.LNil, err
	}

	return extensionRuntime.state.Get(-1), nil
}

func discoverLuaFiles(extensionPath string) ([]string, error) {
	if extensionPath == "" {
		return []string{}, nil
	}

	info, err := os.Stat(extensionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("extension: stat %s: %w", extensionPath, err)
	}

	if !info.IsDir() {
		if strings.HasSuffix(extensionPath, ".lua") {
			return []string{extensionPath}, nil
		}
		return []string{}, nil
	}

	return walkLuaDir(extensionPath)
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
		return nil, fmt.Errorf("extension: walk %s: %w", root, walkErr)
	}
	sort.Strings(files)

	return files, nil
}

func openSafeLibs(state *lua.LState) {
	libraries := []struct {
		open lua.LGFunction
		name string
	}{
		{name: lua.BaseLibName, open: lua.OpenBase},
		{name: lua.TabLibName, open: lua.OpenTable},
		{name: lua.StringLibName, open: lua.OpenString},
		{name: lua.MathLibName, open: lua.OpenMath},
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
