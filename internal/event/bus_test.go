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

const (
	testAgentChannel = "agent"
	testAgentStart   = "agent:start"
	testReadData     = "read"
	testStartData    = "start"
	testToolChannel  = "tool"
	testToolRead     = "tool:read"
)

func TestBus_ReactiveStreams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		subscribe func(*event.Bus, *[]string) event.Unsubscribe
		emit      []event.Envelope
		expected  []string
	}{
		{
			name: "stream receives all envelopes",
			subscribe: func(bus *event.Bus, got *[]string) event.Unsubscribe {
				subscription := bus.Stream().Subscribe(ro.NewObserver(
					func(envelope event.Envelope) {
						*got = append(*got, envelope.Channel+":"+fmt.Sprint(envelope.Data))
					},
					func(error) {},
					func() {},
				))
				return subscription.Unsubscribe
			},
			emit: []event.Envelope{
				{Channel: testAgentChannel, Data: testStartData},
				{Channel: testToolChannel, Data: testReadData},
			},
			expected: []string{testAgentStart, testToolRead},
		},
		{
			name: "channel filters envelopes",
			subscribe: func(bus *event.Bus, got *[]string) event.Unsubscribe {
				subscription := bus.Channel(testAgentChannel).Subscribe(ro.NewObserver(
					func(envelope event.Envelope) {
						*got = append(*got, envelope.Channel+":"+fmt.Sprint(envelope.Data))
					},
					func(error) {},
					func() {},
				))
				return subscription.Unsubscribe
			},
			emit: []event.Envelope{
				{Channel: testToolChannel, Data: "ignored"},
				{Channel: testAgentChannel, Data: testStartData},
			},
			expected: []string{testAgentStart},
		},
		{
			name: "envelope handler receives all channels",
			subscribe: func(bus *event.Bus, got *[]string) event.Unsubscribe {
				return bus.OnEnvelope(func(_ context.Context, envelope event.Envelope) error {
					*got = append(*got, envelope.Channel+":"+fmt.Sprint(envelope.Data))
					return nil
				})
			},
			emit: []event.Envelope{
				{Channel: testAgentChannel, Data: testStartData},
				{Channel: testToolChannel, Data: testReadData},
			},
			expected: []string{testAgentStart, testToolRead},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runReactiveStreamCase(t, testCase.subscribe, testCase.emit, testCase.expected)
		})
	}
}

func runReactiveStreamCase(
	t *testing.T,
	subscribe func(*event.Bus, *[]string) event.Unsubscribe,
	emitted []event.Envelope,
	expected []string,
) {
	t.Helper()

	bus := event.NewBus(testLogger())
	got := []string{}
	unsubscribe := subscribe(bus, &got)
	defer unsubscribe()

	for _, envelope := range emitted {
		bus.Emit(context.Background(), envelope.Channel, envelope.Data)
	}

	assert.ElementsMatch(t, expected, got)
}

func TestBus_OnSubscribesToCurrentSubject(t *testing.T) {
	t.Parallel()

	bus := event.NewBus(testLogger())
	bus.Clear()

	calls := 0
	unsubscribe := bus.On("agent", func(context.Context, any) error {
		calls++
		return nil
	})
	defer unsubscribe()

	bus.Emit(context.Background(), "agent", "start")

	require.Equal(t, 1, calls)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
