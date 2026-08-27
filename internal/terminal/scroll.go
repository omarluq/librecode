package terminal

import (
	"context"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
)

const (
	keyboardScrollRows       = 5
	mouseScrollRows          = 2
	transcriptHydrationBatch = 64
)

type coalescedScrollEvent struct {
	Pending tcell.Event
	Delta   int
}

func (app *App) handleTranscriptScroll(event *tcell.EventKey) bool {
	delta, ok := app.keyScrollDelta(event)
	if !ok {
		return false
	}

	app.scrollTranscript(delta)

	return true
}

func (app *App) handleMouse(event *tcell.EventMouse) {
	if app.mode != modeChat {
		return
	}

	if delta, ok := app.mouseScrollDelta(event); ok {
		app.scrollTranscript(delta)

		return
	}

	column, row := event.Position()
	if event.Buttons()&tcell.ButtonPrimary != 0 {
		if app.selection.active {
			app.updateMouseSelection(column, row)

			return
		}

		app.beginMouseSelection(column, row, event.When())

		return
	}

	if app.selection.active {
		app.finishMouseSelection(column, row)
	}
}

func (app *App) scrollDeltaForEvent(event tcell.Event) (int, bool) {
	if app.mode != modeChat {
		return 0, false
	}

	switch typedEvent := event.(type) {
	case *tcell.EventKey:
		return app.keyScrollDelta(typedEvent)
	case *tcell.EventMouse:
		return app.mouseScrollDelta(typedEvent)
	default:
		return 0, false
	}
}

func (app *App) keyScrollDelta(event *tcell.EventKey) (int, bool) {
	if app.keys.matches(event, actionSelectPageUp) {
		return keyboardScrollRows, true
	}

	if app.keys.matches(event, actionSelectPageDown) {
		return -keyboardScrollRows, true
	}

	return 0, false
}

func (app *App) mouseScrollDelta(event *tcell.EventMouse) (int, bool) {
	buttons := event.Buttons()
	if buttons&tcell.WheelUp != 0 {
		return mouseScrollRows, true
	}

	if buttons&tcell.WheelDown != 0 {
		return -mouseScrollRows, true
	}

	return 0, false
}

func (app *App) coalesceScrollEvents(delta int) coalescedScrollEvent {
	coalesced := coalescedScrollEvent{Pending: nil, Delta: delta}

	for {
		select {
		case event := <-app.screen.EventQ():
			nextDelta, ok := app.scrollDeltaForEvent(event)
			if !ok {
				coalesced.Pending = event

				return coalesced
			}

			coalesced.Delta += nextDelta
		default:
			return coalesced
		}
	}
}

func (app *App) scrollTranscript(delta int) {
	app.scrollOffset = max(0, app.scrollOffset+delta)
}

func (app *App) atLoadedTranscriptStart() bool {
	if !app.transcript.HasOlder || app.transcript.LastMaxRows <= 0 {
		return false
	}

	width := app.currentLineCacheStateWidth()
	app.rebuildMessageRowPrefixSums(width)
	loadedRows := app.transcript.LineCache.prefixes[len(app.transcript.History)]

	return app.scrollOffset >= max(0, loadedRows-app.transcript.LastMaxRows)
}

func (app *App) hydrateOlderTranscript(ctx context.Context) error {
	if !app.transcript.HasOlder || app.runtime == nil || app.sessionID == "" ||
		len(app.transcript.History) == 0 {
		return nil
	}

	oldest := &app.transcript.History[0]
	if oldest.EntryID == nil {
		app.transcript.HasOlder = false

		return nil
	}

	messages, err := app.runtime.SessionRepository().TranscriptMessagesBefore(
		ctx, app.sessionID, oldest.CreatedAt, *oldest.EntryID, transcriptHydrationBatch+1,
	)
	if err != nil {
		return terminalError(err, "load older messages")
	}

	app.transcript.HasOlder = len(messages) > transcriptHydrationBatch
	if app.transcript.HasOlder {
		messages = messages[1:]
	}

	if len(messages) == 0 {
		return nil
	}

	width := app.currentLineCacheStateWidth()
	addedRows := 0

	older := make([]chatMessage, 0, len(messages)+len(app.transcript.History))
	for index := range messages {
		message := chatMessageFromSessionMessage(&messages[index])
		older = append(older, message)
		addedRows += len(app.renderMessage(width, message))
	}

	older = append(older, app.transcript.History...)
	app.transcript.History = older
	app.transcript.LineCache.reset()
	app.scrollOffset += addedRows
	app.prependPromptHistory(messages)

	return nil
}

func (app *App) prependPromptHistory(messages []database.SessionMessageEntity) {
	texts := make([]string, 0, len(messages)+len(app.promptHistory))
	images := make([][]imageAttachment, 0, len(messages)+len(app.promptHistoryImages))

	for index := range messages {
		if messages[index].Role != database.RoleUser {
			continue
		}

		texts = append(texts, messages[index].Content)
		images = append(images, imageAttachmentsFromDatabase(messages[index].Parts))
	}

	texts = append(texts, app.promptHistory...)
	images = append(images, app.promptHistoryImages...)

	if len(texts) > promptHistoryLimit {
		start := len(texts) - promptHistoryLimit
		texts = texts[start:]
		images = images[start:]
	}

	app.promptHistory = texts
	app.promptHistoryImages = images
	app.resetPromptHistoryNavigation()
}
