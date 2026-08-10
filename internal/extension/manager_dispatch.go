package extension

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/omarluq/librecode/internal/tool"

	lua "github.com/yuin/gopher-lua"
)

// ExecuteCommand runs a registered extension slash command.
func (manager *Manager) ExecuteCommand(ctx context.Context, name, args string) (string, error) {
	manager.lock.RLock()
	command, ok := manager.commands[name]
	manager.lock.RUnlock()

	if !ok {
		return "", fmt.Errorf("extension: command %q not found", name)
	}

	if err := ctx.Err(); err != nil {
		return "", extensionError(err, extensionCheckContextStep)
	}

	result, err := callLua(command.extension, command.function, lua.LString(args))
	if err != nil {
		return "", fmt.Errorf("extension: command %q failed: %w", name, err)
	}

	if err := ctx.Err(); err != nil {
		return "", extensionError(err, extensionCheckContextStep)
	}

	return result.String(), nil
}

// ExecuteTool runs a registered extension tool.
func (manager *Manager) ExecuteTool(ctx context.Context, name string, args tool.Arguments) (ToolResult, error) {
	manager.lock.RLock()
	registeredTool, ok := manager.tools[name]
	manager.lock.RUnlock()

	if !ok {
		return ToolResult{Details: map[string]any{}, Content: ""}, fmt.Errorf("extension: tool %q not found", name)
	}

	if err := ctx.Err(); err != nil {
		return ToolResult{Details: map[string]any{}, Content: ""}, extensionError(err, extensionCheckContextStep)
	}

	result, err := callLuaPrepared(
		registeredTool.extension,
		nil,
		registeredTool.function,
		func(state *lua.LState) []lua.LValue {
			return []lua.LValue{toolArgumentsTable(state, args)}
		},
	)
	if err != nil {
		return ToolResult{Details: map[string]any{}, Content: ""},
			fmt.Errorf("extension: tool %q failed: %w", name, err)
	}

	if err := ctx.Err(); err != nil {
		return ToolResult{Details: map[string]any{}, Content: ""}, extensionError(err, extensionCheckContextStep)
	}

	return result.ToolResult(), nil
}

const (
	extensionSlowDispatchThreshold = 100 * time.Millisecond
	extensionDiagnosticWindow      = time.Minute
	extensionDiagnosticAttributes  = 5
)

// HandleTerminalEvent runs registered low-level terminal runtime handlers.
func (manager *Manager) HandleTerminalEvent(
	ctx context.Context,
	event *TerminalEvent,
) (TerminalEventResult, error) {
	if event == nil || event.Name != "tick" && event.Name != "render" {
		return manager.handleTerminalEvent(ctx, event)
	}

	started := manager.dispatchNow()
	result, err := manager.handleTerminalEvent(ctx, event)
	manager.logDispatchDuration(event.Name, manager.dispatchNow().Sub(started), err, 1)

	return result, err
}

func (manager *Manager) handleTerminalEvent(ctx context.Context, event *TerminalEvent) (TerminalEventResult, error) {
	hostEvent := newLuaHostEvent(event)
	if err := manager.runDueTimers(ctx, hostEvent, time.Now()); err != nil {
		return hostEvent.result(), err
	}

	if event.Name == luaFieldKey {
		if err := manager.runKeymaps(ctx, hostEvent); err != nil {
			return hostEvent.result(), err
		}

		if hostEvent.stopped {
			return hostEvent.result(), nil
		}
	}

	for _, handler := range manager.handlersFor(event.Name) {
		if err := ctx.Err(); err != nil {
			return hostEvent.result(), err
		}

		result, err := callLuaPrepared(
			handler.extension,
			hostEvent,
			handler.function,
			func(state *lua.LState) []lua.LValue {
				return []lua.LValue{terminalEventTable(state, hostEvent.eventSnapshot())}
			},
		)
		if err != nil {
			return hostEvent.result(), fmt.Errorf("extension: terminal event %q failed: %w", event.Name, err)
		}

		result.ApplyTo(hostEvent)

		if hostEvent.stopped {
			break
		}
	}

	return hostEvent.result(), nil
}

// Emit sends an event to registered extension handlers.
func (manager *Manager) Emit(ctx context.Context, eventName string, payload map[string]any) error {
	for _, handler := range manager.handlersFor(eventName) {
		if err := ctx.Err(); err != nil {
			return extensionError(err, extensionCheckContextStep)
		}

		_, err := callLuaPrepared(handler.extension, nil, handler.function, func(state *lua.LState) []lua.LValue {
			return []lua.LValue{lua.LString(eventName), mapToLuaTable(state, payload)}
		})
		if err != nil {
			return fmt.Errorf("extension: event %q failed: %w", eventName, err)
		}
	}

	return nil
}

// HasTerminalEventHandlers reports whether any extension handler is registered for eventName.
func (manager *Manager) HasTerminalEventHandlers(eventName string) bool {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	return len(manager.handlers[eventName]) > 0 || eventName == luaFieldKey && len(manager.keymaps) > 0
}

func (manager *Manager) handlersFor(eventName string) []luaHookHandler {
	manager.lock.RLock()
	handlers := append([]luaHookHandler{}, manager.handlers[eventName]...)
	manager.lock.RUnlock()
	sort.SliceStable(handlers, func(leftIndex, rightIndex int) bool {
		left := handlers[leftIndex]

		right := handlers[rightIndex]
		if left.priority == right.priority {
			return left.order < right.order
		}

		return left.priority > right.priority
	})

	return handlers
}

func callLua(extensionRuntime *luaExtension, function *lua.LFunction, args ...lua.LValue) (luaResult, error) {
	return callLuaPrepared(extensionRuntime, nil, function, func(*lua.LState) []lua.LValue {
		return args
	})
}

func callLuaPrepared(
	extensionRuntime *luaExtension,
	hostEvent *luaHostEvent,
	function *lua.LFunction,
	prepareArgs func(*lua.LState) []lua.LValue,
) (luaResult, error) {
	extensionRuntime.lock.Lock()
	defer extensionRuntime.lock.Unlock()

	previousEvent := extensionRuntime.activeEvent

	extensionRuntime.activeEvent = hostEvent
	defer func() {
		extensionRuntime.activeEvent = previousEvent
	}()

	top := extensionRuntime.state.GetTop()

	startedAt := time.Now()
	defer recordLuaCallDuration(extensionRuntime, startedAt)

	args := prepareArgs(extensionRuntime.state)
	if err := extensionRuntime.state.CallByParam(lua.P{Fn: function, NRet: 1, Protect: true}, args...); err != nil {
		extensionRuntime.state.SetTop(top)

		return luaResult{}, extensionError(err, "call lua function")
	}

	result := extensionRuntime.state.Get(-1)
	extensionRuntime.state.Pop(1)
	extensionRuntime.state.SetTop(top)

	return newLuaResult(result), nil
}

func recordLuaCallDuration(extensionRuntime *luaExtension, startedAt time.Time) {
	extensionRuntime.totalDuration.Add(int64(time.Since(startedAt)))
}

func (manager *Manager) logDispatchDuration(
	operation string,
	duration time.Duration,
	err error,
	count int,
) {
	outcome := "success"
	if err != nil {
		outcome = "error"
	}

	attributes := make([]any, 0, extensionDiagnosticAttributes)
	attributes = append(attributes,
		slog.String("operation", operation),
		slog.Duration("duration", duration),
		slog.String("outcome", outcome),
		slog.Int("count", count),
	)
	manager.logger.Debug("extension callback dispatch", attributes...)

	if duration < extensionSlowDispatchThreshold {
		return
	}

	key := operation + ":" + outcome

	manager.diagnosticLock.Lock()
	now := manager.diagnosticNow()

	last := manager.diagnosticLast[key]
	if !last.IsZero() && now.Sub(last) < extensionDiagnosticWindow {
		manager.diagnosticDrops[key]++
		manager.diagnosticLock.Unlock()

		return
	}

	suppressed := manager.diagnosticDrops[key]
	manager.diagnosticLast[key] = now
	manager.diagnosticDrops[key] = 0
	manager.diagnosticLock.Unlock()

	attributes = append(attributes, slog.Int("suppressed_count", suppressed))
	manager.logger.Warn("extension callback dispatch slow", attributes...)
}
