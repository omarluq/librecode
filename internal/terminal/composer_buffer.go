package terminal

import (
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/terminal/input"
)

func composerBufferFromExtension(buffer *extension.BufferState) input.Buffer {
	nextBuffer := input.NewBuffer()
	if buffer == nil {
		return nextBuffer
	}
	nextBuffer.Metadata = extui.CloneMetadata(buffer.Metadata)
	nextBuffer.Text = buffer.Text
	nextBuffer.Chars = input.StringChars(buffer.Text)
	nextBuffer.Label = buffer.Label
	nextBuffer.Cursor = input.ClampCursor(buffer.Cursor, len([]rune(buffer.Text)))

	return nextBuffer
}

func extensionBufferFromComposer(buffer input.Buffer) extension.BufferState {
	return extension.BufferState{
		Metadata: extui.CloneMetadata(buffer.Metadata),
		Blocks:   []extension.BufferBlock{},
		Name:     extui.BufferComposer,
		Text:     buffer.Text,
		Label:    buffer.Label,
		Chars:    append([]string{}, buffer.Chars...),
		Cursor:   input.ClampCursor(buffer.Cursor, len([]rune(buffer.Text))),
	}
}
