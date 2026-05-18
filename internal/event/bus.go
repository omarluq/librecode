// Package event provides a small librecode-style in-process event bus backed by samber/ro.
package event

import (
	"context"
	"log/slog"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/oops"
	"github.com/samber/ro"
)

// Handler processes one event payload for a channel.
type Handler func(ctx context.Context, data any) error

// EnvelopeHandler processes one full event envelope.
type EnvelopeHandler func(ctx context.Context, envelope Envelope) error

// Unsubscribe removes a previously registered handler.
type Unsubscribe func()

// Envelope is the value emitted through the reactive event stream.
type Envelope struct {
	Data    any    `json:"data"`
	Channel string `json:"channel"`
}

// Bus publishes payloads to handlers registered by channel.
type Bus struct {
	logger        *slog.Logger
	subject       ro.Subject[Envelope]
	subscriptions map[uint64]ro.Subscription
	lock          sync.Mutex
	nextID        uint64
}

// NewBus creates an event bus. A nil logger discards handler failures.
func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		logger:        logger,
		subject:       ro.NewPublishSubject[Envelope](),
		subscriptions: map[uint64]ro.Subscription{},
		lock:          sync.Mutex{},
		nextID:        0,
	}
}

// Emit delivers data to the current reactive subject for channel.
func (bus *Bus) Emit(ctx context.Context, channel string, data any) {
	bus.lock.Lock()
	subject := bus.subject
	bus.lock.Unlock()

	subject.NextWithContext(ctx, Envelope{Data: data, Channel: channel})
}

// Stream returns the hot event stream backing this bus.
//
// The stream is observational only: subscribers must not mutate runtime state in
// ways that require an ordered result. Use explicit middleware dispatchers for
// allow/reject/modify decisions.
func (bus *Bus) Stream() ro.Observable[Envelope] {
	bus.lock.Lock()
	defer bus.lock.Unlock()

	return bus.subject.AsObservable()
}

// Channel returns a filtered view of Stream for one channel.
func (bus *Bus) Channel(channel string) ro.Observable[Envelope] {
	return ro.Pipe1(
		bus.Stream(),
		ro.Filter(func(envelope Envelope) bool {
			return envelope.Channel == channel
		}),
	)
}

// On registers a channel handler and returns an unsubscribe function.
func (bus *Bus) On(channel string, handler Handler) Unsubscribe {
	observer := ro.NewObserverWithContext(
		func(ctx context.Context, envelope Envelope) {
			if err := handler(ctx, envelope.Data); err != nil {
				bus.logHandlerError(envelope.Channel, err)
			}
		},
		func(_ context.Context, err error) {
			bus.logHandlerError(channel, err)
		},
		ignoreObserverComplete,
	)

	return bus.subscribe(func(subject ro.Subject[Envelope]) ro.Observable[Envelope] {
		return ro.Pipe1(
			subject.AsObservable(),
			ro.Filter(func(envelope Envelope) bool {
				return envelope.Channel == channel
			}),
		)
	}, observer)
}

// OnEnvelope registers a handler for every emitted envelope.
func (bus *Bus) OnEnvelope(handler EnvelopeHandler) Unsubscribe {
	observer := ro.NewObserverWithContext(
		func(ctx context.Context, envelope Envelope) {
			if err := handler(ctx, envelope); err != nil {
				bus.logHandlerError(envelope.Channel, err)
			}
		},
		func(_ context.Context, err error) {
			bus.logHandlerError("*", err)
		},
		ignoreObserverComplete,
	)

	return bus.subscribe(func(subject ro.Subject[Envelope]) ro.Observable[Envelope] {
		return subject.AsObservable()
	}, observer)
}

// Clear removes all registered handlers and rotates the hot subject.
func (bus *Bus) Clear() {
	bus.lock.Lock()
	defer bus.lock.Unlock()

	lo.ForEach(lo.Values(bus.subscriptions), func(subscription ro.Subscription, _ int) {
		subscription.Unsubscribe()
	})
	bus.subscriptions = map[uint64]ro.Subscription{}
	bus.subject.Complete()
	bus.subject = ro.NewPublishSubject[Envelope]()
}

func (bus *Bus) subscribe(
	observableFor func(ro.Subject[Envelope]) ro.Observable[Envelope],
	observer ro.Observer[Envelope],
) Unsubscribe {
	bus.lock.Lock()
	defer bus.lock.Unlock()

	bus.nextID++
	subscriptionID := bus.nextID
	subscription := observableFor(bus.subject).Subscribe(observer)
	bus.subscriptions[subscriptionID] = subscription

	return func() {
		bus.unsubscribe(subscriptionID)
	}
}

func (bus *Bus) unsubscribe(subscriptionID uint64) {
	bus.lock.Lock()
	defer bus.lock.Unlock()

	subscription, ok := bus.subscriptions[subscriptionID]
	if !ok {
		return
	}
	subscription.Unsubscribe()
	delete(bus.subscriptions, subscriptionID)
}

func (bus *Bus) logHandlerError(channel string, err error) {
	if bus.logger == nil {
		return
	}

	wrapped := oops.
		In("event").
		Code("handler_failed").
		With("channel", channel).
		Wrapf(err, "event handler failed")
	bus.logger.Error("event handler failed", slog.Any("error", wrapped))
}

func ignoreObserverComplete(context.Context) {
	// Clear rotates the hot subject; registered handlers have no completion cleanup.
}
