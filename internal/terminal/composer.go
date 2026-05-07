package terminal

import (
	"context"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
)

const composerKeyCtrlR = "ctrl+r"

type composer struct {
	runner extension.ComposerRunner
	mode   string
	label  string
}

func newComposer(mode, label string, runner extension.ComposerRunner) *composer {
	if mode == "" || runner == nil {
		return nil
	}
	if label == "" {
		label = mode
	}

	return &composer{
		runner: runner,
		mode:   mode,
		label:  label,
	}
}

func (app *App) handleComposerKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if app.composer == nil {
		return false, nil
	}
	if event.Key() == tcell.KeyEscape && (app.working || app.authWorking) {
		return false, nil
	}

	keyEvent, ok := composerKeyEvent(event)
	if !ok {
		return false, nil
	}

	result, err := app.composer.runner.HandleComposerKey(
		ctx,
		app.composer.mode,
		keyEvent,
		app.composerState(),
	)
	if err != nil {
		return false, err
	}
	app.applyComposerResult(result)

	return result.Handled, nil
}

func (app *App) composerState() extension.ComposerState {
	return extension.ComposerState{
		Text:        app.editor.text(),
		Chars:       editorChars(app.editor.value),
		Cursor:      app.editor.cursor,
		Working:     app.working,
		AuthWorking: app.authWorking,
	}
}

func editorChars(value []rune) []string {
	chars := make([]string, 0, len(value))
	for _, char := range value {
		chars = append(chars, string(char))
	}

	return chars
}

func (app *App) applyComposerResult(result extension.ComposerResult) {
	if app.composer == nil {
		return
	}
	if result.Label != "" {
		app.composer.label = result.Label
	}

	oldText := app.editor.text()
	if result.HasText {
		app.editor.setText(result.Text)
	}
	if result.HasCursor {
		app.editor.cursor = result.Cursor
	}
	app.editor.cursor = min(max(0, app.editor.cursor), len(app.editor.value))
	if result.HasText && result.Text != oldText {
		app.resetPromptHistoryNavigation()
	}
}

func (app *App) composerFooterLabel() string {
	if app.composer == nil {
		return ""
	}

	return app.composer.label
}

func composerKeyEvent(event *tcell.EventKey) (extension.ComposerKeyEvent, bool) {
	if event.Key() == tcell.KeyRune {
		return composerRuneKeyEvent(event), true
	}

	key, ok := composerSpecialKeys()[event.Key()]
	if !ok {
		var keyEvent extension.ComposerKeyEvent

		return keyEvent, false
	}

	return extension.ComposerKeyEvent{
		Key:  key,
		Text: "",
		Ctrl: strings.HasPrefix(key, "ctrl+"),
		Alt:  event.Modifiers()&tcell.ModAlt != 0,
	}, true
}

func composerRuneKeyEvent(event *tcell.EventKey) extension.ComposerKeyEvent {
	text := event.Str()
	ctrl := event.Modifiers()&tcell.ModCtrl != 0
	key := text
	if ctrl {
		key = "ctrl+" + strings.ToLower(text)
	}

	return extension.ComposerKeyEvent{
		Key:  key,
		Text: text,
		Ctrl: ctrl,
		Alt:  event.Modifiers()&tcell.ModAlt != 0,
	}
}

func composerSpecialKeys() map[tcell.Key]string {
	return map[tcell.Key]string{
		tcell.KeyEscape: "escape",
		tcell.KeyEnter:  "enter",
		tcell.KeyLeft:   "left",
		tcell.KeyRight:  "right",
		tcell.KeyUp:     "up",
		tcell.KeyDown:   "down",
		tcell.KeyHome:   "home",
		tcell.KeyEnd:    "end",
		tcell.KeyCtrlR:  composerKeyCtrlR,
	}
}
