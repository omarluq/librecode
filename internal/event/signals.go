package event

import (
	"context"
	"os"
	"os/signal"

	"github.com/samber/ro"
	rosignal "github.com/samber/ro/plugins/signal"
)

// SignalStream returns an ro observable of process signals. It delegates to the
// samber/ro signal plugin so process shutdown uses the same reactive event
// primitives as the rest of the runtime.
func SignalStream(signals ...os.Signal) ro.Observable[os.Signal] {
	return rosignal.NewSignalCatcher(signals...)
}

// SignalContext returns a child context canceled by the first matching process
// signal.
func SignalContext(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, signals...)
}
