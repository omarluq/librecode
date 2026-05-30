package extension

import (
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	lua "github.com/yuin/gopher-lua"
)

type luaExtension struct {
	activeEvent   *luaHostEvent
	state         *lua.LState
	name          string
	path          string
	commands      []string
	tools         []string
	keymaps       []string
	handlers      []string
	lock          sync.Mutex
	totalDuration atomic.Int64
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
	priority  int
	order     uint64
}

type luaKeymap struct {
	extension   *luaExtension
	function    *lua.LFunction
	target      keymapTarget
	lhs         string
	description string
	priority    int
	order       uint64
}

type luaTimer struct {
	extension *luaExtension
	function  *lua.LFunction
	due       time.Time
	interval  time.Duration
	id        uint64
	order     uint64
}

// Manager is the built-in Lua runtime adapter for the extension host.
type Manager struct {
	logger           *slog.Logger
	commands         map[string]luaCommand
	tools            map[string]luaTool
	handlers         map[string][]luaHookHandler
	keymaps          []luaKeymap
	namespaces       map[string]int
	canceledTimers   map[uint64]struct{}
	moduleRoots      []string
	timers           []luaTimer
	extensions       []*luaExtension
	lock             sync.RWMutex
	nextHandlerOrder uint64
	nextTimerID      uint64
	nextNamespaceID  int
}

// NewManager creates an empty Lua runtime adapter.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		logger:           logger,
		commands:         map[string]luaCommand{},
		tools:            map[string]luaTool{},
		handlers:         map[string][]luaHookHandler{},
		keymaps:          []luaKeymap{},
		namespaces:       map[string]int{},
		canceledTimers:   map[uint64]struct{}{},
		moduleRoots:      []string{},
		timers:           []luaTimer{},
		extensions:       []*luaExtension{},
		lock:             sync.RWMutex{},
		nextHandlerOrder: 0,
		nextTimerID:      1,
		nextNamespaceID:  1,
	}
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
			Keymaps:       append([]string{}, extensionRuntime.keymaps...),
			Handlers:      append([]string{}, extensionRuntime.handlers...),
			Timers:        manager.extensionTimerCount(extensionRuntime),
			TotalDuration: time.Duration(extensionRuntime.totalDuration.Load()),
		})
	}

	return extensions
}

// Commands returns registered extension commands sorted by name.
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

// Tools returns registered extension tools sorted by name.
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

// Shutdown closes all loaded Lua states and clears registrations.
func (manager *Manager) Shutdown() {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	for _, extensionRuntime := range manager.extensions {
		extensionRuntime.state.Close()
	}
	manager.extensions = []*luaExtension{}
	manager.commands = map[string]luaCommand{}
	manager.tools = map[string]luaTool{}
	manager.handlers = map[string][]luaHookHandler{}
	manager.keymaps = []luaKeymap{}
	manager.namespaces = map[string]int{}
	manager.canceledTimers = map[uint64]struct{}{}
	manager.moduleRoots = []string{}
	manager.timers = []luaTimer{}
	manager.nextHandlerOrder = 0
	manager.nextTimerID = 1
	manager.nextNamespaceID = 1
}

func (manager *Manager) unregisterRuntime(extensionRuntime *luaExtension) {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	manager.unregisterRuntimeLocked(extensionRuntime)
}

func (manager *Manager) unregisterRuntimeLocked(extensionRuntime *luaExtension) {
	manager.unregisterCommandsLocked(extensionRuntime)
	manager.unregisterToolsLocked(extensionRuntime)
	manager.unregisterHandlersLocked(extensionRuntime)
	manager.unregisterKeymapsLocked(extensionRuntime)
	manager.unregisterTimersLocked(extensionRuntime)
	manager.unregisterExtensionLocked(extensionRuntime)
}

func (manager *Manager) unregisterCommandsLocked(extensionRuntime *luaExtension) {
	for _, name := range extensionRuntime.commands {
		if command, ok := manager.commands[name]; ok && command.extension == extensionRuntime {
			delete(manager.commands, name)
		}
	}
}

func (manager *Manager) unregisterToolsLocked(extensionRuntime *luaExtension) {
	for _, name := range extensionRuntime.tools {
		if tool, ok := manager.tools[name]; ok && tool.extension == extensionRuntime {
			delete(manager.tools, name)
		}
	}
}

func (manager *Manager) unregisterHandlersLocked(extensionRuntime *luaExtension) {
	for eventName, handlers := range manager.handlers {
		filtered := keepHandlersFromOtherRuntimes(handlers, extensionRuntime)
		if len(filtered) == 0 {
			delete(manager.handlers, eventName)
			continue
		}
		manager.handlers[eventName] = filtered
	}
}

func keepHandlersFromOtherRuntimes(handlers []luaHookHandler, extensionRuntime *luaExtension) []luaHookHandler {
	filtered := handlers[:0]
	for _, handler := range handlers {
		if handler.extension != extensionRuntime {
			filtered = append(filtered, handler)
		}
	}

	return filtered
}

func (manager *Manager) unregisterKeymapsLocked(extensionRuntime *luaExtension) {
	filtered := manager.keymaps[:0]
	for _, keymap := range manager.keymaps {
		if keymap.extension != extensionRuntime {
			filtered = append(filtered, keymap)
		}
	}
	manager.keymaps = filtered
}

func (manager *Manager) unregisterTimersLocked(extensionRuntime *luaExtension) {
	filtered := manager.timers[:0]
	for _, timer := range manager.timers {
		if timer.extension != extensionRuntime {
			filtered = append(filtered, timer)
			continue
		}
		manager.canceledTimers[timer.id] = struct{}{}
	}
	manager.timers = filtered
}

func (manager *Manager) unregisterExtensionLocked(extensionRuntime *luaExtension) {
	filtered := manager.extensions[:0]
	for _, loadedRuntime := range manager.extensions {
		if loadedRuntime != extensionRuntime {
			filtered = append(filtered, loadedRuntime)
		}
	}
	manager.extensions = filtered
}
