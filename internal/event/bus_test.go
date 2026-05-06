package event_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/event"
)

func TestBus_EmitCallsHandlers(t *testing.T) {
	t.Parallel()

	bus := event.NewBus(testLogger())
	calls := []string{}
	bus.On("agent", func(_ context.Context, data any) error {
		calls = append(calls, "first:"+fmt.Sprint(data))
		return nil
	})
	bus.On("agent", func(_ context.Context, data any) error {
		calls = append(calls, "second:"+fmt.Sprint(data))
		return nil
	})

	bus.Emit(context.Background(), "agent", "start")

	assert.ElementsMatch(t, []string{"first:start", "second:start"}, calls)
}

func TestBus_UnsubscribeAndClear(t *testing.T) {
	t.Parallel()

	bus := event.NewBus(testLogger())
	calls := 0
	unsubscribe := bus.On("agent", func(context.Context, any) error {
		calls++
		return nil
	})
	unsubscribe()
	bus.Emit(context.Background(), "agent", nil)

	bus.On("agent", func(context.Context, any) error {
		calls++
		return nil
	})
	bus.Clear()
	bus.Emit(context.Background(), "agent", nil)

	assert.Zero(t, calls)
}

func TestBus_EmitLogsHandlerErrorsAndContinues(t *testing.T) {
	t.Parallel()

	bus := event.NewBus(testLogger())
	calls := 0
	bus.On("agent", func(context.Context, any) error {
		return errors.New("boom")
	})
	bus.On("agent", func(context.Context, any) error {
		calls++
		return nil
	})

	bus.Emit(context.Background(), "agent", nil)

	assert.Equal(t, 1, calls)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
