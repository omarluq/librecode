//nolint:testpackage // These tests exercise unexported queue/status helpers.
package terminal

import (
	"slices"
	"testing"
)

const testQueuedPromptText = "next prompt"

func TestQueueFollowUpText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "text", text: testQueuedPromptText, want: []string{testQueuedPromptText}},
		{name: "trimmed text", text: "  " + testQueuedPromptText + "  ", want: []string{testQueuedPromptText}},
		{name: "empty", text: "", want: nil},
		{name: "whitespace only", text: "  \n\t", want: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			app.setStatus("ready")

			app.queueFollowUpText(testCase.text)

			if got, want := app.statusMessage, "ready"; got != want {
				t.Fatalf("statusMessage = %q, want %q", got, want)
			}
			if got := app.queuedMessages; !slices.Equal(got, testCase.want) {
				t.Fatalf("queuedMessages = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestQueueFollowUpRequiresText(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.queueFollowUp()

	if got, want := app.statusMessage, "no follow-up text to queue"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}
	if len(app.queuedMessages) != 0 {
		t.Fatalf("queuedMessages length = %d, want 0", len(app.queuedMessages))
	}
}

func TestQueueFollowUpRecordsAndClearsComposer(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setComposerText("  follow me  ")

	app.queueFollowUp()

	if got, want := app.composerText(), ""; got != want {
		t.Fatalf("composer text = %q, want empty", got)
	}
	if got, want := app.queuedMessages, []string{"follow me"}; !slices.Equal(got, want) {
		t.Fatalf("queuedMessages = %v, want %v", got, want)
	}
	if got, want := app.promptHistory, []string{"follow me"}; !slices.Equal(got, want) {
		t.Fatalf("promptHistory = %v, want %v", got, want)
	}
}

func TestDequeueFollowUpRestoresLastMessage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.queuedMessages = []string{"first", "second"}
	app.promptHistoryIndex = 1

	app.dequeueFollowUp()

	if got, want := app.composerText(), "second"; got != want {
		t.Fatalf("composer text = %q, want %q", got, want)
	}
	if got, want := app.queuedMessages, []string{"first"}; !slices.Equal(got, want) {
		t.Fatalf("queuedMessages = %v, want %v", got, want)
	}
	if got, want := app.statusMessage, "restored queued message"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}
}

func TestDequeueFollowUpHandlesEmptyQueue(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.dequeueFollowUp()

	if got, want := app.statusMessage, "no queued messages"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}
}

func TestBoolText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		want  string
		value bool
	}{
		{name: "true", want: "on", value: true},
		{name: "false", want: boolTextOff, value: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := boolText(tt.value); got != tt.want {
				t.Fatalf("boolText(%t) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
