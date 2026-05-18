package event_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/samber/ro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestBus_StreamExposesReactiveEnvelopeStream(t *testing.T) {
	t.Parallel()

	bus := event.NewBus(testLogger())
	envelopes := []event.Envelope{}
	subscription := bus.Stream().Subscribe(ro.NewObserver(
		func(envelope event.Envelope) {
			envelopes = append(envelopes, envelope)
		},
		func(error) {},
		func() {},
	))
	defer subscription.Unsubscribe()

	bus.Emit(context.Background(), "agent", "start")
	bus.Emit(context.Background(), "tool", "read")

	require.Len(t, envelopes, 2)
	assert.Equal(t, event.Envelope{Channel: "agent", Data: "start"}, envelopes[0])
	assert.Equal(t, event.Envelope{Channel: "tool", Data: "read"}, envelopes[1])
}

func TestBus_ChannelReturnsFilteredReactiveStream(t *testing.T) {
	t.Parallel()

	bus := event.NewBus(testLogger())
	channels := []string{}
	subscription := bus.Channel("agent").Subscribe(ro.NewObserver(
		func(envelope event.Envelope) {
			channels = append(channels, envelope.Channel+":"+fmt.Sprint(envelope.Data))
		},
		func(error) {},
		func() {},
	))
	defer subscription.Unsubscribe()

	bus.Emit(context.Background(), "tool", "ignored")
	bus.Emit(context.Background(), "agent", "start")

	assert.Equal(t, []string{"agent:start"}, channels)
}

func TestBus_OnEnvelopeReceivesAllChannels(t *testing.T) {
	t.Parallel()

	bus := event.NewBus(testLogger())
	calls := []string{}
	bus.OnEnvelope(func(_ context.Context, envelope event.Envelope) error {
		calls = append(calls, envelope.Channel+":"+fmt.Sprint(envelope.Data))
		return nil
	})

	bus.Emit(context.Background(), "agent", "start")
	bus.Emit(context.Background(), "tool", "read")

	assert.ElementsMatch(t, []string{"agent:start", "tool:read"}, calls)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
