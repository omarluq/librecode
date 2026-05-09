//nolint:testpackage // These tests exercise unexported queue/status helpers.
package terminal

import "testing"

func TestQueueFollowUpDoesNotSetStatus(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setStatus("ready")

	app.queueFollowUpText("next prompt")

	if got, want := app.statusMessage, "ready"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}
	if got, want := len(app.queuedMessages), 1; got != want {
		t.Fatalf("queuedMessages length = %d, want %d", got, want)
	}
}
