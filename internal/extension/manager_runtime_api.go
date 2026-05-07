package extension

import (
	"context"
	"fmt"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

const (
	keymapModeGlobal   = "global"
	keymapModeComposer = "composer"
	keymapWildcard     = "*"
)

func (manager *Manager) luaCoreAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"create_autocmd":           manager.luaCreateAutocmd(extensionRuntime),
		"nvim_create_autocmd":      manager.luaCreateAutocmd(extensionRuntime),
		"create_namespace":         manager.luaCreateNamespace(),
		"nvim_create_namespace":    manager.luaCreateNamespace(),
		"create_user_command":      manager.luaCreateUserCommand(extensionRuntime),
		"nvim_create_user_command": manager.luaCreateUserCommand(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaAutocmdAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		luaFieldCreate: manager.luaCreateAutocmd(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaCommandAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		luaFieldCreate: manager.luaCreateUserCommand(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaKeymapAPI(extensionRuntime *luaExtension) *lua.LTable {
	state := extensionRuntime.state
	apiTable := state.NewTable()
	state.SetFuncs(apiTable, map[string]lua.LGFunction{
		"set": manager.luaKeymapSet(extensionRuntime),
		"del": manager.luaKeymapDel(extensionRuntime),
	})

	return apiTable
}

func (manager *Manager) luaCreateAutocmd(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		events := luaEventNames(state.CheckAny(1))
		priority, function := luaAutocmdArgs(state)
		for _, eventName := range events {
			manager.registerHandler(extensionRuntime, eventName, priority, function)
		}

		return 0
	}
}

func (manager *Manager) luaCreateNamespace() lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		manager.lock.Lock()
		defer manager.lock.Unlock()

		if namespaceID, ok := manager.namespaces[name]; ok {
			state.Push(lua.LNumber(namespaceID))
			return 1
		}

		namespaceID := manager.nextNamespaceID
		manager.nextNamespaceID++
		manager.namespaces[name] = namespaceID
		state.Push(lua.LNumber(namespaceID))

		return 1
	}
}

func (manager *Manager) luaCreateUserCommand(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		name := state.CheckString(1)
		description, function := luaUserCommandArgs(state)
		definition := Command{Name: name, Description: description, Extension: extensionRuntime.name}

		manager.lock.Lock()
		manager.commands[name] = luaCommand{extension: extensionRuntime, function: function, definition: definition}
		extensionRuntime.commands = append(extensionRuntime.commands, name)
		manager.lock.Unlock()

		return 0
	}
}

func (manager *Manager) luaKeymapSet(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		modes := luaKeymapModes(state.CheckAny(1))
		lhs := normalizeKeySpec(state.CheckString(2))
		function := state.CheckFunction(3)
		options := state.OptTable(4, state.NewTable())
		priority := int(lua.LVAsNumber(options.RawGetString("priority")))
		description := luaTableString(options, "desc", luaTableString(options, "description", ""))

		for _, mode := range modes {
			manager.registerKeymap(extensionRuntime, mode, lhs, description, priority, function)
		}

		return 0
	}
}

func (manager *Manager) luaKeymapDel(extensionRuntime *luaExtension) lua.LGFunction {
	return func(state *lua.LState) int {
		modes := luaKeymapModes(state.CheckAny(1))
		lhs := normalizeKeySpec(state.CheckString(2))
		modeSet := map[string]struct{}{}
		labels := map[string]struct{}{}
		for _, mode := range modes {
			modeSet[mode] = struct{}{}
			labels[mode+":"+lhs] = struct{}{}
		}

		manager.lock.Lock()
		defer manager.lock.Unlock()

		keymaps := manager.keymaps[:0]
		for _, keymap := range manager.keymaps {
			_, sameMode := modeSet[keymap.mode]
			if keymap.extension == extensionRuntime && sameMode && keymap.lhs == lhs {
				continue
			}
			keymaps = append(keymaps, keymap)
		}
		manager.keymaps = keymaps
		extensionRuntime.keymaps = removeKeymapLabels(extensionRuntime.keymaps, labels)

		return 0
	}
}

func removeKeymapLabels(keymaps []string, labels map[string]struct{}) []string {
	filtered := keymaps[:0]
	for _, keymap := range keymaps {
		if _, ok := labels[keymap]; ok {
			continue
		}
		filtered = append(filtered, keymap)
	}

	return filtered
}

func (manager *Manager) registerHandler(
	extensionRuntime *luaExtension,
	eventName string,
	priority int,
	function *lua.LFunction,
) {
	manager.lock.Lock()
	manager.nextHandlerOrder++
	manager.handlers[eventName] = append(manager.handlers[eventName], luaHookHandler{
		extension: extensionRuntime,
		function:  function,
		priority:  priority,
		order:     manager.nextHandlerOrder,
	})
	manager.lock.Unlock()
}

func (manager *Manager) registerKeymap(
	extensionRuntime *luaExtension,
	mode string,
	lhs string,
	description string,
	priority int,
	function *lua.LFunction,
) {
	mode = normalizeKeymapMode(mode)
	manager.lock.Lock()
	manager.nextHandlerOrder++
	manager.keymaps = append(manager.keymaps, luaKeymap{
		extension:   extensionRuntime,
		function:    function,
		mode:        mode,
		lhs:         lhs,
		description: description,
		priority:    priority,
		order:       manager.nextHandlerOrder,
	})
	extensionRuntime.keymaps = append(extensionRuntime.keymaps, mode+":"+lhs)
	manager.lock.Unlock()
}

func (manager *Manager) runKeymaps(ctx context.Context, event *luaHostEvent) error {
	for _, keymap := range manager.keymapsFor(event) {
		if err := ctx.Err(); err != nil {
			return err
		}

		result, err := callLuaPrepared(
			keymap.extension,
			event,
			keymap.function,
			func(state *lua.LState) []lua.LValue {
				return []lua.LValue{terminalEventTable(state, event.eventSnapshot())}
			},
		)
		if err != nil {
			return fmt.Errorf("extension: keymap %s %q failed: %w", keymap.mode, keymap.lhs, err)
		}
		if result == lua.LTrue {
			event.consumed = true
		}
		event.applyLuaResult(result)
		if event.stopped {
			return nil
		}
	}

	return nil
}

func (manager *Manager) keymapsFor(event *luaHostEvent) []luaKeymap {
	manager.lock.RLock()
	keymaps := append([]luaKeymap{}, manager.keymaps...)
	manager.lock.RUnlock()

	eventKey := normalizeKeySpec(event.key.Key)
	eventModes := keymapEventModes(event)
	matched := make([]luaKeymap, 0, len(keymaps))
	for _, keymap := range keymaps {
		if keymap.lhs != keymapWildcard && keymap.lhs != eventKey {
			continue
		}
		if _, ok := eventModes[keymap.mode]; !ok {
			continue
		}
		matched = append(matched, keymap)
	}
	sort.SliceStable(matched, func(leftIndex, rightIndex int) bool {
		left := matched[leftIndex]
		right := matched[rightIndex]
		if left.priority == right.priority {
			return left.order < right.order
		}

		return left.priority > right.priority
	})

	return matched
}

func keymapEventModes(event *luaHostEvent) map[string]struct{} {
	modes := map[string]struct{}{keymapModeGlobal: {}}
	if mode, ok := event.context["mode"].(string); ok && mode != "" {
		modes[normalizeKeymapMode(mode)] = struct{}{}
	}
	if _, ok := event.buffers[keymapModeComposer]; ok {
		modes[keymapModeComposer] = struct{}{}
	}

	return modes
}

func luaAutocmdArgs(state *lua.LState) (priority int, function *lua.LFunction) {
	second := state.CheckAny(2)
	if handler, ok := second.(*lua.LFunction); ok {
		return 0, handler
	}

	options := state.CheckTable(2)
	priority = int(lua.LVAsNumber(options.RawGetString("priority")))
	function = luaTableFunction(options, "callback")
	if function == nil {
		function = luaTableFunction(options, "command")
	}
	if function == nil {
		state.RaiseError("autocmd callback must be a function")
	}

	return priority, function
}

func luaUserCommandArgs(state *lua.LState) (description string, function *lua.LFunction) {
	second := state.CheckAny(2)
	if handler, ok := second.(*lua.LFunction); ok {
		return "", handler
	}

	options := state.CheckTable(2)
	description = luaTableString(options, "desc", luaTableString(options, "description", ""))
	function = luaTableFunction(options, "callback")
	if function == nil {
		state.RaiseError("command callback must be a function")
	}

	return description, function
}

func luaEventNames(value lua.LValue) []string {
	if table, ok := value.(*lua.LTable); ok {
		return luaStringSlice(table)
	}

	return []string{value.String()}
}

func luaKeymapModes(value lua.LValue) []string {
	if table, ok := value.(*lua.LTable); ok {
		modes := luaStringSlice(table)
		for index, mode := range modes {
			modes[index] = normalizeKeymapMode(mode)
		}

		return modes
	}

	return []string{normalizeKeymapMode(value.String())}
}

func normalizeKeymapMode(mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return keymapModeGlobal
	}

	return mode
}

func normalizeKeySpec(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		return ""
	}
	if strings.HasPrefix(key, "<") && strings.HasSuffix(key, ">") {
		key = strings.TrimSuffix(strings.TrimPrefix(key, "<"), ">")
	}
	key = strings.ReplaceAll(key, "c-", "ctrl+")
	key = strings.ReplaceAll(key, "ctrl-", "ctrl+")
	key = strings.ReplaceAll(key, "control-", "ctrl+")
	key = strings.ReplaceAll(key, "control+", "ctrl+")

	switch key {
	case "*", "any":
		return keymapWildcard
	case "esc":
		return "escape"
	case "cr", "return":
		return "enter"
	case "bs":
		return "backspace"
	case "space":
		return " "
	default:
		return key
	}
}
