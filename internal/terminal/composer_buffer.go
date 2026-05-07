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

func (app *App) composerEditor() *editor {
	input := newEditor()
	input.value = []rune(app.composerBuffer.Text)
	input.cursor = clampComposerCursor(app.composerBuffer.Cursor, len(input.value))

	return input
}

func (app *App) applyComposerEditor(input *editor) {
	app.composerBuffer.Text = input.text()
	app.composerBuffer.Chars = editorChars(input.value)
	app.composerBuffer.Cursor = input.cursor
}

func (app *App) editComposer(mutator func(*editor)) {
	input := app.composerEditor()
	mutator(input)
	app.applyComposerEditor(input)
}

func (app *App) setComposerText(text string) {
	app.composerBuffer.Text = text
	app.composerBuffer.Chars = stringBufferChars(text)
	app.composerBuffer.Cursor = len([]rune(text))
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
