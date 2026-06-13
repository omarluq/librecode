package terminal

import (
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/mapsutil"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/tui"
)

func composerBufferFromExtension(buffer *extension.BufferState) tui.TextArea {
	nextBuffer := tui.NewTextArea()
	if buffer == nil {
		return nextBuffer
	}

	nextBuffer.Metadata = mapsutil.CloneOrEmpty(buffer.Metadata)
	nextBuffer.Text = buffer.Text
	nextBuffer.Chars = tui.StringChars(buffer.Text)
	nextBuffer.Label = buffer.Label
	nextBuffer.Cursor = tui.ClampCursor(buffer.Cursor, len([]rune(buffer.Text)))

	return nextBuffer
}

func extensionBufferFromComposer(buffer tui.TextArea) extension.BufferState {
	return extension.BufferState{
		Metadata: mapsutil.CloneOrEmpty(buffer.Metadata),
		Blocks:   []extension.BufferBlock{},
		Name:     extui.BufferComposer,
		Text:     buffer.Text,
		Label:    buffer.Label,
		Chars:    append([]string{}, buffer.Chars...),
		Cursor:   tui.ClampCursor(buffer.Cursor, len([]rune(buffer.Text))),
	}
}
