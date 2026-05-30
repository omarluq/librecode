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
