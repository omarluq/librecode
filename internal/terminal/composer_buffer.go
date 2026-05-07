package terminal

import "github.com/omarluq/librecode/internal/extension"

func newComposerBuffer() extension.BufferState {
	return textBufferState(extensionBufferComposer, "")
}

func (app *App) composerText() string {
	return app.composerBuffer.Text
}

func (app *App) composerCursor() int {
	return app.composerBuffer.Cursor
}

func (app *App) composerEmpty() bool {
	return app.composerBuffer.Text == ""
}

func (app *App) composerBorderLabel() string {
	return app.composerLabel()
}

func (app *App) composerLabel() string {
	return app.composerBuffer.Label
}

func (app *App) setComposerRunes(value []rune, cursor int) {
	text := string(value)
	app.composerBuffer.Text = text
	app.composerBuffer.Chars = editorChars(value)
	app.composerBuffer.Cursor = clampComposerCursor(cursor, len(value))
}

func (app *App) updateComposer(mutator func([]rune, int) ([]rune, int)) {
	value := []rune(app.composerBuffer.Text)
	cursor := clampComposerCursor(app.composerBuffer.Cursor, len(value))
	nextValue, nextCursor := mutator(value, cursor)
	app.setComposerRunes(nextValue, nextCursor)
}

func (app *App) insertComposerRune(char rune) {
	app.updateComposer(func(value []rune, cursor int) ([]rune, int) {
		return insertRuneAt(value, cursor, char)
	})
}

func (app *App) moveComposerLeft() {
	app.updateComposer(func(value []rune, cursor int) ([]rune, int) {
		return value, moveCursorLeft(value, cursor)
	})
}

func (app *App) moveComposerRight() {
	app.updateComposer(func(value []rune, cursor int) ([]rune, int) {
		return value, moveCursorRight(value, cursor)
	})
}

func (app *App) moveComposerWordLeft() {
	app.updateComposer(func(value []rune, cursor int) ([]rune, int) {
		return value, moveCursorWordLeft(value, cursor)
	})
}

func (app *App) moveComposerWordRight() {
	app.updateComposer(func(value []rune, cursor int) ([]rune, int) {
		return value, moveCursorWordRight(value, cursor)
	})
}

func (app *App) moveComposerLineStart() {
	app.updateComposer(func(value []rune, cursor int) ([]rune, int) {
		return value, moveCursorLineStart(value, cursor)
	})
}

func (app *App) moveComposerLineEnd() {
	app.updateComposer(func(value []rune, cursor int) ([]rune, int) {
		return value, moveCursorLineEnd(value, cursor)
	})
}

func (app *App) deleteComposerBackward() {
	app.updateComposer(backspaceAt)
}

func (app *App) deleteComposerForward() {
	app.updateComposer(deleteForwardAt)
}

func (app *App) deleteComposerWordBackward() {
	app.updateComposer(deleteWordBackwardAt)
}

func (app *App) deleteComposerWordForward() {
	app.updateComposer(deleteWordForwardAt)
}

func (app *App) deleteComposerToLineStart() {
	app.updateComposer(deleteToLineStartAt)
}

func (app *App) deleteComposerToLineEnd() {
	app.updateComposer(deleteToLineEndAt)
}

func (app *App) setComposerText(text string) {
	app.setComposerRunes([]rune(text), len([]rune(text)))
}

func (app *App) clearComposer() string {
	text := app.composerText()
	app.setComposerText("")

	return text
}

func (app *App) setComposerBuffer(buffer *extension.BufferState) {
	nextBuffer := newComposerBuffer()
	if buffer != nil {
		nextBuffer.Metadata = cloneExtensionMetadata(buffer.Metadata)
		nextBuffer.Name = extensionBufferComposer
		nextBuffer.Text = buffer.Text
		nextBuffer.Chars = stringBufferChars(buffer.Text)
		nextBuffer.Label = buffer.Label
		nextBuffer.Cursor = clampComposerCursor(buffer.Cursor, len([]rune(buffer.Text)))
	}
	app.composerBuffer = nextBuffer
}

func cloneBufferState(buffer *extension.BufferState) extension.BufferState {
	cloned := *buffer
	cloned.Metadata = cloneExtensionMetadata(cloned.Metadata)
	cloned.Chars = append([]string{}, cloned.Chars...)
	if cloned.Name == "" {
		cloned.Name = extensionBufferComposer
	}
	if cloned.Chars == nil {
		cloned.Chars = stringBufferChars(cloned.Text)
	}
	cloned.Cursor = clampComposerCursor(cloned.Cursor, len([]rune(cloned.Text)))

	return cloned
}

func clampComposerCursor(cursor, runeCount int) int {
	if cursor < 0 {
		return 0
	}
	if cursor > runeCount {
		return runeCount
	}

	return cursor
}
