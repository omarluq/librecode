package terminal

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
)

const (
	testQueuedImageName  = "paste.png"
	testQueuedPromptText = "next prompt"
)

func TestQueueFollowUpText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "plain prompt", text: testQueuedPromptText, want: []string{testQueuedPromptText}},
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

			if got := app.queuedMessages; !slices.Equal(promptDraftTexts(got), testCase.want) {
				t.Fatalf("queuedMessages = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestQueueFollowUpRejectsImagesForTextOnlyModel(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.models = model.NewRegistryContext(t.Context(), &model.RegistryOptions{
		ConfigReader: nil, Auth: nil, ModelsPath: "",
		BuiltIns:  []model.Model{terminalCapabilityTestModel(app.currentProvider(), app.currentModel())},
		Discovery: disabledModelDiscovery(),
	})
	app.composerImages = []imageAttachment{{
		Name: testQueuedImageName, MIMEType: clipboardImageMIME, Data: []byte{1}, Width: 1, Height: 1,
	}}
	app.composerBuffer.SetText("follow-up")

	app.queueFollowUp()

	if len(app.queuedMessages) != 0 {
		t.Fatalf("queuedMessages length = %d, want 0", len(app.queuedMessages))
	}

	if len(app.composerImages) != 1 {
		t.Fatalf("composerImages length = %d, want 1", len(app.composerImages))
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
	app.composerBuffer.SetText("  follow me  ")

	app.queueFollowUp()

	if got, want := app.composerBuffer.TextValue(), ""; got != want {
		t.Fatalf("composer text = %q, want empty", got)
	}

	if got, want := app.queuedMessages, []string{"follow me"}; !slices.Equal(promptDraftTexts(got), want) {
		t.Fatalf("queuedMessages = %v, want %v", got, want)
	}

	if got, want := app.promptHistory, []string{"follow me"}; !slices.Equal(got, want) {
		t.Fatalf("promptHistory = %v, want %v", got, want)
	}
}

func TestQueueAndDequeueFollowUpPreserveImages(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	image := imageAttachment{
		Name: testQueuedImageName, MIMEType: clipboardImageMIME, Data: []byte{1, 2}, Width: 1, Height: 1,
	}

	app.composerBuffer.SetText("follow me")
	app.composerImages = []imageAttachment{image}

	app.queueFollowUp()
	require.Len(t, app.queuedMessages, 1)
	require.Len(t, app.queuedMessages[0].Images, 1)
	assert.Equal(t, image.Data, app.queuedMessages[0].Images[0].Data)
	assert.True(t, app.composerDraftEmpty())

	image.Data[0] = 9
	assert.Equal(t, byte(1), app.queuedMessages[0].Images[0].Data[0])

	app.dequeueFollowUp()
	assert.Equal(t, "follow me", app.composerBuffer.TextValue())
	require.Len(t, app.composerImages, 1)
	assert.Equal(t, []byte{1, 2}, app.composerImages[0].Data)
	assert.Empty(t, app.queuedMessages)
}

func TestQueueAndDequeueImageOnlyFollowUp(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerImages = []imageAttachment{{
		Name: testQueuedImageName, MIMEType: clipboardImageMIME, Data: []byte{1}, Width: 1, Height: 1,
	}}

	app.queueFollowUp()
	require.Len(t, app.queuedMessages, 1)
	assert.Empty(t, app.queuedMessages[0].Text)
	assert.True(t, app.composerDraftEmpty())

	app.dequeueFollowUp()
	assert.Empty(t, app.composerBuffer.TextValue())
	require.Len(t, app.composerImages, 1)
	assert.Equal(t, []byte{1}, app.composerImages[0].Data)
}

func TestDequeueFollowUpRestoresLastMessage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.queuedMessages = promptDrafts("first", "second")
	app.promptHistoryIndex = 1

	app.dequeueFollowUp()

	if got, want := app.composerBuffer.TextValue(), "second"; got != want {
		t.Fatalf("composer text = %q, want %q", got, want)
	}

	if got, want := app.queuedMessages, []string{"first"}; !slices.Equal(promptDraftTexts(got), want) {
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

func TestProcessQueuedPrompt(t *testing.T) {
	t.Parallel()

	const (
		firstQueuedPrompt     = "one"
		secondQueuedPrompt    = "two"
		sendFirstQueuedPrompt = "sends first queued prompt"
	)

	tests := []struct {
		setup       func(*App)
		name        string
		wantQueued  []string
		wantWorking bool
	}{
		{
			name: "busy leaves queue unchanged",
			setup: func(app *App) {
				app.working = true
				app.queuedMessages = promptDrafts(firstQueuedPrompt)
			},
			wantQueued:  []string{firstQueuedPrompt},
			wantWorking: true,
		},
		{
			name:        "empty queue leaves idle",
			setup:       func(*App) {},
			wantQueued:  nil,
			wantWorking: false,
		},
		{
			name:        sendFirstQueuedPrompt,
			setup:       func(app *App) { app.queuedMessages = promptDrafts(firstQueuedPrompt, secondQueuedPrompt) },
			wantQueued:  []string{secondQueuedPrompt},
			wantWorking: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := newTerminalPromptClient(newTerminalCompletionResult("ok"), nil)

			app := newPromptSendTestApp(t, client)
			if testCase.name == sendFirstQueuedPrompt {
				app.screen = newClipboardScreen()
			}

			testCase.setup(app)

			app.processQueuedPrompt(context.Background())

			if testCase.name == sendFirstQueuedPrompt {
				_ = readPromptAsyncEvent(t, app)
			}

			if !slices.Equal(promptDraftTexts(app.queuedMessages), testCase.wantQueued) {
				t.Fatalf("queuedMessages = %v, want %v", app.queuedMessages, testCase.wantQueued)
			}

			if app.working != testCase.wantWorking {
				t.Fatalf("working = %v, want %v", app.working, testCase.wantWorking)
			}
		})
	}
}
