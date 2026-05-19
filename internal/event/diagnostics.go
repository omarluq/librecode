package event

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/samber/ro"
)

// DiagnosticObserver logs envelopes from the reactive event stream.
type DiagnosticObserver struct {
	subscription ro.Subscription
	logger       *slog.Logger
}

// NewDiagnosticObserver subscribes to the bus event stream and logs lifecycle
// traffic for debugging/diagnostics. It is observational only and never mutates
// event payloads.
func NewDiagnosticObserver(bus *Bus, logger *slog.Logger) *DiagnosticObserver {
	observer := &DiagnosticObserver{logger: logger, subscription: nil}
	if bus == nil || logger == nil {
		return observer
	}
	stream := ro.Pipe1(
		bus.Stream(),
		ro.TapOnNextWithContext(func(_ context.Context, envelope Envelope) {
			logger.Debug(
				"runtime event",
				slog.String("channel", envelope.Channel),
				slog.String("data_type", dataType(envelope.Data)),
			)
		}),
	)
	observer.subscription = stream.Subscribe(ro.NewObserverWithContext(
		func(context.Context, Envelope) {
			// Intentionally no-op: logging happens in the TapOnNext stage.
		},
		func(_ context.Context, err error) {
			logger.Debug("runtime event stream failed", slog.Any("error", err))
		},
		func(context.Context) {
			logger.Debug("runtime event stream completed")
		},
	))

	return observer
}

// Stop unsubscribes the diagnostic observer.
func (observer *DiagnosticObserver) Stop() {
	if observer == nil || observer.subscription == nil {
		return
	}
	observer.subscription.Unsubscribe()
	observer.subscription = nil
}

func dataType(data any) string {
	if data == nil {
		return "<nil>"
	}

	return reflect.TypeOf(data).String()
}
