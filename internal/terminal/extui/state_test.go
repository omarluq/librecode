package extui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
)

const (
	testWindowName  = "main"
	testMetadataKey = "kind"
	testMetadataVal = "input"
)

func TestState_ApplyLayoutClonesWindows(t *testing.T) {
	t.Parallel()

	state := extui.NewState()
	layout := &extension.LayoutState{
		Windows: map[string]extension.WindowState{
			testWindowName: testWindowState(testWindowName, 20),
		},
		Width:  80,
		Height: 24,
	}

	state.ApplyLayout(layout)
	layout.Windows[testWindowName] = testWindowState(testWindowName, 1)

	require.NotNil(t, state.Layout)
	assert.Equal(t, 80, state.Layout.Width)
	assert.Equal(t, testWindowName, state.Layout.Windows[testWindowName].Name)
	assert.Equal(t, 20, state.Windows[testWindowName].Width)
}

func TestState_DeleteWindowRemovesLayoutOverrideAndCursor(t *testing.T) {
	t.Parallel()

	state := extui.NewState()
	state.ApplyLayout(&extension.LayoutState{
		Windows: map[string]extension.WindowState{
			testWindowName: testWindowState(testWindowName, 10),
		},
		Width:  80,
		Height: 24,
	})
	state.ResetWindowOverride(testWindowName)
	state.SetCursor(&extension.UICursor{Window: testWindowName, Row: 1, Col: 2})

	state.DeleteWindow(testWindowName)

	_, hasWindow := state.Windows[testWindowName]
	_, hasLayoutWindow := state.Layout.Windows[testWindowName]
	_, hasOverride := state.Overrides[testWindowName]
	assert.False(t, hasWindow)
	assert.False(t, hasLayoutWindow)
	assert.False(t, hasOverride)
	assert.Nil(t, state.Cursor)
}

func TestCloneBufferNormalizesFields(t *testing.T) {
	t.Parallel()

	buffer := extui.CloneBuffer("composer", &extension.BufferState{
		Metadata: map[string]any{testMetadataKey: testMetadataVal},
		Blocks:   []extension.BufferBlock{},
		Name:     "",
		Text:     "héllo",
		Label:    "",
		Chars:    []string{},
		Cursor:   99,
	})

	assert.Equal(t, "composer", buffer.Name)
	assert.Equal(t, map[string]any{testMetadataKey: testMetadataVal}, buffer.Metadata)
	assert.Equal(t, []string{"h", "é", "l", "l", "o"}, buffer.Chars)
	assert.Equal(t, 5, buffer.Cursor)

	original := &extension.BufferState{
		Metadata: map[string]any{testMetadataKey: testMetadataVal},
		Blocks:   []extension.BufferBlock{},
		Name:     "",
		Text:     "héllo",
		Label:    "",
		Chars:    []string{"h"},
		Cursor:   0,
	}
	cloned := extui.CloneBuffer("composer", original)
	cloned.Metadata[testMetadataKey] = "mutated"
	cloned.Chars[0] = "x"

	assert.Equal(t, testMetadataVal, original.Metadata[testMetadataKey])
	assert.Equal(t, "h", original.Chars[0])
}

func TestState_AppendDrawOpIgnoresEmptyWindow(t *testing.T) {
	t.Parallel()

	state := extui.NewState()
	state.AppendDrawOp(&extension.UIDrawOp{
		Window: "",
		Kind:   "",
		Text:   "ignored",
		Style:  testUIStyle(),
		Spans:  []extension.UISpan{},
		Row:    0,
		Col:    0,
		Width:  0,
		Height: 0,
		Clear:  false,
	})
	state.AppendDrawOp(&extension.UIDrawOp{
		Window: testWindowName,
		Kind:   "",
		Text:   "kept",
		Style:  testUIStyle(),
		Spans:  []extension.UISpan{},
		Row:    0,
		Col:    0,
		Width:  0,
		Height: 0,
		Clear:  false,
	})

	require.Len(t, state.Overrides, 1)
	assert.Equal(t, "kept", state.Overrides[testWindowName].DrawOps[0].Text)
}

func testUIStyle() extension.UIStyle {
	return extension.UIStyle{FG: "", BG: "", Bold: false, Italic: false}
}

func testWindowState(name string, width int) extension.WindowState {
	return extension.WindowState{
		Metadata:  map[string]any{},
		Name:      name,
		Role:      "",
		Buffer:    "",
		Renderer:  "",
		X:         0,
		Y:         0,
		Width:     width,
		Height:    0,
		CursorRow: 0,
		CursorCol: 0,
		Visible:   true,
	}
}
