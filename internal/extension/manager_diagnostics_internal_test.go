package extension

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lua "github.com/yuin/gopher-lua"
)

func TestExtensionDispatchDiagnosticsAreBoundedContentFreeAndRateLimited(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	manager := NewManager(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	now := time.Unix(1, 0)
	manager.diagnosticNow = func() time.Time { return now }

	manager.logDispatchDuration("render", time.Millisecond, nil, 1)
	manager.logDispatchDuration("render", extensionSlowDispatchThreshold, errors.New("private payload"), 1)
	manager.logDispatchDuration("render", extensionSlowDispatchThreshold, errors.New("private payload"), 1)

	output := logs.String()
	assert.Contains(t, output, "level=DEBUG")
	assert.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("level=WARN")))
	assert.Contains(t, output, "operation=render")
	assert.Contains(t, output, "outcome=error")
	assert.NotContains(t, output, "private payload")

	now = now.Add(extensionDiagnosticWindow)

	manager.logDispatchDuration("render", extensionSlowDispatchThreshold, errors.New("private payload"), 1)
	assert.Contains(t, logs.String(), "suppressed_count=1")
}

func TestExtensionTickAndRenderDiagnosticsIncludeLockWait(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"tick", "render"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer

			manager, extensionRuntime := newDiagnosticTestManager(t, &logs)
			manager.handlers[operation] = []luaHookHandler{{
				extension: extensionRuntime,
				function:  extensionRuntime.state.NewFunction(func(_ *lua.LState) int { return 0 }),
				priority:  0,
				order:     0,
			}}

			assertDispatchIncludesLockWait(t, manager, extensionRuntime, func() error {
				_, err := manager.HandleTerminalEvent(context.Background(), diagnosticTerminalEvent(operation))

				return err
			})

			output := logs.String()
			assert.Contains(t, output, "operation="+operation)
			assert.Contains(t, output, "outcome=success")
			assert.Contains(t, output, "duration=100ms")
			assert.Contains(t, output, "count=1")
			assert.NotContains(t, output, "private-event-content")
		})
	}
}

func TestExtensionTimerDiagnosticsIncludeLockWait(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	manager, extensionRuntime := newDiagnosticTestManager(t, &logs)
	manager.timers = []luaTimer{{
		extension: extensionRuntime,
		function:  extensionRuntime.state.NewFunction(func(_ *lua.LState) int { return 0 }),
		due:       time.Unix(1, 0),
		interval:  0,
		id:        1,
		order:     0,
	}}

	assertDispatchIncludesLockWait(t, manager, extensionRuntime, func() error {
		_, err := manager.HandleTerminalEvent(context.Background(), diagnosticTerminalEvent("key"))

		return err
	})

	output := logs.String()
	assert.Contains(t, output, "operation=timer")
	assert.Contains(t, output, "outcome=success")
	assert.Contains(t, output, "duration=100ms")
	assert.Contains(t, output, "count=1")
	assert.NotContains(t, output, "private-event-content")
}

func newDiagnosticTestManager(t *testing.T, logs *bytes.Buffer) (*Manager, *luaExtension) {
	t.Helper()

	manager := NewManager(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	state := lua.NewState()
	extensionRuntime := &luaExtension{
		activeEvent:   nil,
		state:         state,
		name:          "diagnostic",
		path:          "",
		commands:      nil,
		lock:          sync.Mutex{},
		tools:         nil,
		keymaps:       nil,
		handlers:      nil,
		totalDuration: atomic.Int64{},
	}
	manager.extensions = append(manager.extensions, extensionRuntime)
	t.Cleanup(manager.Shutdown)

	return manager, extensionRuntime
}

func diagnosticTerminalEvent(operation string) *TerminalEvent {
	return &TerminalEvent{
		Buffers: map[string]BufferState{},
		Windows: map[string]WindowState{},
		Context: map[string]any{},
		Data:    map[string]any{"payload": "private-event-content"},
		Name:    operation,
		Key: ComposerKeyEvent{
			Key: "", Text: "", Ctrl: false, Alt: false, Shift: false,
		},
		Layout: LayoutState{Windows: nil, Width: 0, Height: 0},
		Focus: FocusState{
			Kind: "", Window: "", Buffer: "", Role: "", PanelKind: "", Exclusive: false,
		},
	}
}

func assertDispatchIncludesLockWait(
	t *testing.T,
	manager *Manager,
	extensionRuntime *luaExtension,
	dispatch func() error,
) {
	t.Helper()

	startedAt := time.Unix(10, 0)

	times := make(chan time.Time, 2)
	times <- startedAt

	times <- startedAt.Add(extensionSlowDispatchThreshold)

	started := make(chan struct{})
	manager.dispatchNow = func() time.Time {
		select {
		case now := <-times:
			if now.Equal(startedAt) {
				close(started)
			}

			return now
		default:
			t.Error("unexpected dispatchNow call")

			return startedAt
		}
	}

	extensionRuntime.lock.Lock()

	done := make(chan error, 1)
	go func() { done <- dispatch() }()

	<-started

	select {
	case err := <-done:
		extensionRuntime.lock.Unlock()
		require.NoError(t, err)
		t.Fatal("dispatch completed while the extension lock was held")
	default:
	}

	extensionRuntime.lock.Unlock()
	require.NoError(t, <-done)
	assert.Empty(t, times, "expected dispatch to consume every timestamp")
}
