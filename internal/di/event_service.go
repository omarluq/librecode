package di

import (
	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/event"
)

// EventService exposes the process-wide event bus.
type EventService struct {
	Bus *event.Bus
}

// NewEventService wires the in-process event bus.
func NewEventService(injector do.Injector) (*EventService, error) {
	logger := do.MustInvoke[*LoggerService](injector).SlogLogger

	return &EventService{Bus: event.NewBus(logger)}, nil
}

// Shutdown clears all event handlers.
func (service *EventService) Shutdown() {
	service.Bus.Clear()
}
