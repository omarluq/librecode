// Package event provides a small Pi-style in-process event bus.
package event

import (
	"context"
	"log/slog"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/oops"
)

// Handler processes one event payload for a channel.
type Handler func(ctx context.Context, data any) error

// Unsubscribe removes a previously registered handler.
type Unsubscribe func()

// Bus publishes payloads to handlers registered by channel.
type Bus struct {
	logger        *slog.Logger
	subscriptions map[string][]subscription
	lock          sync.RWMutex
	nextID        uint64
}

type subscription struct {
	handler Handler
	id      uint64
}

// NewBus creates an event bus. A nil logger discards handler failures.
func NewBus(logger *slog.Logger) *Bus {
	return &Bus{
		logger:        logger,
		subscriptions: map[string][]subscription{},
		lock:          sync.RWMutex{},
		nextID:        0,
	}
}

// Emit delivers data to the current handlers for channel.
func (bus *Bus) Emit(ctx context.Context, channel string, data any) {
	handlers := bus.handlers(channel)
	for _, handler := range handlers {
		if err := ctx.Err(); err != nil {
			bus.logHandlerError(channel, err)
			return
		}
		if err := handler(ctx, data); err != nil {
			bus.logHandlerError(channel, err)
		}
	}
}

// On registers a handler and returns an unsubscribe function.
func (bus *Bus) On(channel string, handler Handler) Unsubscribe {
	bus.lock.Lock()
	bus.nextID++
	id := bus.nextID
	bus.subscriptions[channel] = append(bus.subscriptions[channel], subscription{handler: handler, id: id})
	bus.lock.Unlock()

	return func() {
		bus.unsubscribe(channel, id)
	}
}

// Clear removes all registered handlers.
func (bus *Bus) Clear() {
	bus.lock.Lock()
	defer bus.lock.Unlock()

	bus.subscriptions = map[string][]subscription{}
}

func (bus *Bus) handlers(channel string) []Handler {
	bus.lock.RLock()
	defer bus.lock.RUnlock()

	return lo.Map(bus.subscriptions[channel], func(subscription subscription, _ int) Handler {
		return subscription.handler
	})
}

func (bus *Bus) unsubscribe(channel string, id uint64) {
	bus.lock.Lock()
	defer bus.lock.Unlock()

	bus.subscriptions[channel] = lo.Reject(bus.subscriptions[channel], func(subscription subscription, _ int) bool {
		return subscription.id == id
	})
	if len(bus.subscriptions[channel]) == 0 {
		delete(bus.subscriptions, channel)
	}
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
