package terminal

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const testImageAttachmentName = "image.png"

type imageClipboardStub struct {
	err  error
	data []byte
}

func (stub imageClipboardStub) ReadImage() ([]byte, error) { return stub.data, stub.err }

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, width, height))))

	return buffer.Bytes()
}

func TestClipboardImagePasteValidateRemoveAndClear(t *testing.T) {
	t.Parallel()
	app := newRenderTestApp(t)
	data := testPNG(t, 3, 2)
	app.imageClipboard = imageClipboardStub{err: nil, data: data}

	assert.True(t, app.pasteClipboardImage())
	require.Len(t, app.composerImages, 1)
	assert.Equal(t, "paste-1.png", app.composerImages[0].Name)
	assert.Equal(t, 3, app.composerImages[0].Width)
	assert.Equal(t, "attached paste-1.png", app.statusMessage)

	data[0] ^= 0xff
	assert.NotEqual(t, data[0], app.composerImages[0].Data[0])

	assert.True(t, app.handleAttachmentKey(tcell.NewEventKey(tcell.KeyRune, "r", tcell.ModAlt)))
	assert.Empty(t, app.composerImages)
	app.composerImages = []imageAttachment{
		{Name: "first-image", MIMEType: "", Data: nil, Width: 0, Height: 0},
		{Name: "second-image", MIMEType: "", Data: nil, Width: 0, Height: 0},
	}
	assert.True(t, app.handleAttachmentKey(tcell.NewEventKey(tcell.KeyRune, "c", tcell.ModAlt)))
	assert.Empty(t, app.composerImages)
}

func TestClipboardImageFailuresPreserveDraft(t *testing.T) {
	t.Parallel()
	app := newRenderTestApp(t)
	app.composerImages = []imageAttachment{{Name: "existing", MIMEType: "", Data: []byte{1}, Width: 0, Height: 0}}
	app.imageClipboard = imageClipboardStub{err: nil, data: []byte("not png")}
	assert.True(t, app.pasteClipboardImage())
	assert.Equal(t, "existing", app.composerImages[0].Name)
	assert.Contains(t, app.statusMessage, "decode clipboard image")

	app.imageClipboard = imageClipboardStub{err: nil, data: nil}
	assert.False(t, app.pasteClipboardImage())
	require.Len(t, app.composerImages, 1)
}

func TestClipboardImageLimits(t *testing.T) {
	t.Parallel()
	pngData := testPNG(t, 1, 1)
	_, err := validateClipboardPNG(make([]byte, maxComposerImageBytes+1), nil)
	require.ErrorContains(t, err, "5 MiB")
	_, err = validateClipboardPNG(pngData, make([]imageAttachment, maxComposerImages))
	require.ErrorContains(t, err, "limit is 4")

	existing := imageAttachment{
		Name: "", MIMEType: "", Data: make([]byte, maxComposerImageTotal), Width: 0, Height: 0,
	}
	_, err = validateClipboardPNG(pngData, []imageAttachment{existing})
	require.ErrorContains(t, err, "20 MiB")
}

func TestBracketedPasteInsertsLiterally(t *testing.T) {
	t.Parallel()
	app := newRenderTestApp(t)
	ctx := context.Background()
	_, err := app.handleEvent(ctx, tcell.NewEventPaste(true))
	require.NoError(t, err)

	for _, event := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyRune, "/", tcell.ModNone),
		tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModNone),
		tcell.NewEventKey(tcell.KeyTab, "", tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
	} {
		_, err = app.handleEvent(ctx, event)
		require.NoError(t, err)
	}

	_, err = app.handleEvent(ctx, tcell.NewEventPaste(false))
	require.NoError(t, err)
	assert.Equal(t, "/\n\tx", app.composerBuffer.TextValue())
	assert.False(t, app.working)
}

func TestPromptDraftAndSessionViewCloneImageBytes(t *testing.T) {
	t.Parallel()
	app := newRenderTestApp(t)
	app.sessionID = benchmarkDisplayedSession
	app.composerImages = []imageAttachment{{Name: "image", MIMEType: "", Data: []byte{1, 2}, Width: 0, Height: 0}}
	app.promptHistory = []string{"history"}
	app.promptHistoryImages = [][]imageAttachment{{{
		Name: "history-image", MIMEType: "", Data: []byte{4}, Width: 0, Height: 0,
	}}}
	queuedImage := imageAttachment{Name: "", MIMEType: "", Data: []byte{3}, Width: 0, Height: 0}
	app.queuedMessages = []promptDraft{{Text: "queued", Images: []imageAttachment{queuedImage}}}
	app.saveSessionView()
	app.composerImages[0].Data[0] = 9
	app.promptHistory[0] = "changed"
	app.promptHistoryImages[0][0].Data[0] = 9
	app.queuedMessages[0].Images[0].Data[0] = 9
	view := app.sessionViews[benchmarkDisplayedSession]
	assert.Equal(t, byte(1), view.composerImages[0].Data[0])
	assert.Equal(t, "history", view.promptHistory[0])
	assert.Equal(t, byte(4), view.promptHistoryImages[0][0].Data[0])
	assert.Equal(t, byte(3), view.queuedMessages[0].Images[0].Data[0])
}

func TestDraftModelCapabilityValidation(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.models = nil
	draft := promptDraft{Text: "", Images: []imageAttachment{{
		Name: "", MIMEType: clipboardImageMIME, Data: []byte{1}, Width: 1, Height: 1,
	}}}
	assert.True(t, app.validateDraftModel(draft))

	app.models = model.NewRegistry(&model.RegistryOptions{
		ConfigReader: nil, Auth: nil, ModelsPath: "",
		BuiltIns:  []model.Model{terminalCapabilityTestModel(app.currentProvider(), app.currentModel())},
		Discovery: disabledModelDiscovery(),
	})
	assert.False(t, app.validateDraftModel(draft))
	assert.Equal(t, "selected model does not support image input", app.statusMessage)
}

func terminalCapabilityTestModel(provider, id string) model.Model {
	return model.Model{
		ThinkingLevelMap: nil, Headers: nil, Compat: nil, Provider: provider, ID: id,
		Name: id, API: "", BaseURL: "", Input: []model.InputMode{model.InputText},
		Cost:          model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow: 0, MaxTokens: 0, Reasoning: false,
	}
}

func TestImageOnlyDraftIsNotTreatedAsEmptyAndComposerLayoutIsBounded(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerImages = []imageAttachment{
		{Name: "image-a", MIMEType: clipboardImageMIME, Data: []byte{1}, Width: 1, Height: 1},
		{Name: "image-b", MIMEType: clipboardImageMIME, Data: []byte{2}, Width: 1, Height: 1},
		{Name: "image-c", MIMEType: clipboardImageMIME, Data: []byte{3}, Width: 1, Height: 1},
	}
	assert.False(t, app.composerDraftEmpty())
	rendered := app.renderComposerEditor(40, 2)
	assert.LessOrEqual(t, len(rendered.Lines), 2+composerBorderRows)
	assert.Contains(t, rendered.Lines[1].Text, "3 more attachments")

	app.handleEscapePresses(t.Context(), 1)
	assert.True(t, app.composerDraftEmpty())
	assert.Equal(t, "editor cleared", app.statusMessage)
}

func TestControlCClearsImageOnlyDraftBeforeExit(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.composerImages = []imageAttachment{{
		Name: testImageAttachmentName, MIMEType: clipboardImageMIME, Data: []byte{1}, Width: 1, Height: 1,
	}}

	shouldQuit, err := app.handleKey(
		t.Context(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModCtrl),
	)
	require.NoError(t, err)
	assert.False(t, shouldQuit)
	assert.True(t, app.composerDraftEmpty())
}

func TestReadOnlyInspectionBlocksImageAndBracketedPaste(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = "child"
	app.activePrompt = newTestActivePrompt(func() {})
	app.activePrompt.SessionID = sessionCommandsParentID
	app.working = true
	app.imageClipboard = imageClipboardStub{err: nil, data: testPNG(t, 1, 1)}

	app.bracketedPaste = true
	_, err := app.handleEvent(
		t.Context(), tcell.NewEventKey(tcell.KeyRune, "x", tcell.ModNone),
	)
	require.NoError(t, err)
	assert.False(t, app.bracketedPaste)
	assert.Empty(t, app.composerBuffer.TextValue())

	_, err = app.handleEvent(t.Context(), tcell.NewEventPaste(true))
	require.NoError(t, err)
	assert.False(t, app.bracketedPaste)

	shouldQuit, err := app.handleKey(
		t.Context(), tcell.NewEventKey(tcell.KeyRune, "v", tcell.ModCtrl),
	)
	require.NoError(t, err)
	assert.False(t, shouldQuit)
	assert.Empty(t, app.composerImages)
	assert.Equal(t, readOnlyAgentInspectionStatus, app.statusMessage)
}

func TestAppendSessionMessagesRestoresAttachmentSummaries(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.appendSessionMessages([]database.SessionMessageEntity{{
		CreatedAt: time.Time{}, ID: "", SessionID: "", EntryID: "", Sender: "",
		Role: database.RoleUser, Content: "", Provider: "", Model: "",
		Parts: []database.MessagePartEntity{{
			Type: database.MessagePartImage, Text: "", Name: testImageAttachmentName,
			MIMEType: clipboardImageMIME, Data: []byte("image"), Width: 10, Height: 20,
		}},
	}})

	require.Len(t, app.transcript.History, 1)
	summaries := app.transcript.History[0].Attachments
	require.NotNil(t, summaries)
	require.Len(t, *summaries, 1)
	assert.Equal(t, testImageAttachmentName, (*summaries)[0].Name)
	assert.Equal(t, clipboardImageMIME, (*summaries)[0].MIMEType)
	assert.Equal(t, 10, (*summaries)[0].Width)
	assert.Equal(t, 20, (*summaries)[0].Height)
	assert.Equal(t, len("image"), (*summaries)[0].Size)
}

func TestAttachmentRenderingContainsMetadataNotData(t *testing.T) {
	t.Parallel()
	app := newRenderTestApp(t)
	secret := []byte("RAW-SECRET")
	attachment := imageAttachment{Name: "paste-1.png", MIMEType: "image/png", Data: secret, Width: 10, Height: 20}
	app.composerImages = []imageAttachment{attachment}
	composer := app.attachmentChipLines(80)
	require.Len(t, composer, 1)
	assert.Contains(t, composer[0].Text, "paste-1.png")
	assert.NotContains(t, composer[0].Text, string(secret))
	assert.True(t, strings.HasPrefix(composer[0].Text, "│ "))
	assert.True(t, strings.HasSuffix(composer[0].Text, " │"))

	lines := app.renderUserMessage(80, "", *summarizeAttachments([]imageAttachment{attachment}))

	var rendered strings.Builder
	for _, line := range lines {
		rendered.WriteString(line.Text)
	}

	assert.Contains(t, rendered.String(), "10×20")
	assert.NotContains(t, rendered.String(), string(secret))
}
