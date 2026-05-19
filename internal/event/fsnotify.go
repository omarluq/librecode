package event

import (
	"github.com/fsnotify/fsnotify"
	"github.com/samber/ro"
	rofsnotify "github.com/samber/ro/plugins/fsnotify"
)

// FileEvent is one fsnotify event emitted through FileWatchStream.
type FileEvent struct {
	Err   error          `json:"error,omitempty"`
	Event fsnotify.Event `json:"event"`
}

// FileWatchStream watches paths and emits fsnotify events through ro. It wraps
// the samber/ro fsnotify plugin so config and extension reload pipelines share
// the same reactive event primitives as the rest of the runtime.
func FileWatchStream(paths ...string) ro.Observable[FileEvent] {
	return ro.Pipe1(
		rofsnotify.NewFSListener(paths...),
		ro.Map(func(event fsnotify.Event) FileEvent {
			return FileEvent{Err: nil, Event: event}
		}),
	)
}
