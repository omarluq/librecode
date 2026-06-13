package extui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
)

func TestState_BufferLifecycleAndFrameReset(t *testing.T) {
	t.Parallel()

	state := extui.NewState()
	state.ApplyBuffer(extui.BufferComposer, &extension.BufferState{
		Metadata: map[string]any{testMetadataKey: testMetadataVal},
		Blocks:   []extension.BufferBlock{},
		Name:     "",
		Text:     "draft",
		Label:    "",
		Chars:    []string{},
		Cursor:   -5,
	})

	buffer, found := state.RuntimeBuffer(extui.BufferComposer)
	require.True(t, found)
	assert.Equal(t, "draft", buffer.Text)
	assert.Equal(t, 0, buffer.Cursor)
	buffer.Metadata[testMetadataKey] = "mutated"

	again, found := state.RuntimeBuffer(extui.BufferComposer)
	require.True(t, found)
	assert.Equal(t, testMetadataVal, again.Metadata[testMetadataKey])

	state.AppendDrawOp(&extension.UIDrawOp{
		Window: testWindowName,
		Kind:   "",
		Text:   "draw",
		Style:  testUIStyle(),
		Spans:  []extension.UISpan{},
		Row:    0,
		Col:    0,
		Width:  0,
		Height: 0,
		Clear:  false,
	})
	state.SetCursor(&extension.UICursor{Window: testWindowName, Row: 1, Col: 2})
	state.ResetFrameOverrides()
	assert.Empty(t, state.Overrides)
	assert.Nil(t, state.Cursor)

	state.DeleteBuffer(extui.BufferComposer)
	_, found = state.RuntimeBuffer(extui.BufferComposer)
	assert.False(t, found)
}

func TestState_ApplyWindowMirrorsActiveLayout(t *testing.T) {
	t.Parallel()

	state := extui.NewState()
	state.ApplyLayout(&extension.LayoutState{Windows: map[string]extension.WindowState{}, Width: 80, Height: 24})

	window := testWindowState("", 30)

	state.ApplyWindow(testWindowName, &window)

	assert.Equal(t, testWindowName, state.Windows[testWindowName].Name)
	require.NotNil(t, state.Layout)
	assert.Equal(t, testWindowName, state.Layout.Windows[testWindowName].Name)
	assert.Equal(t, 30, state.Layout.Windows[testWindowName].Width)

	state.ApplyWindow("ignored", nil)
	_, ok := state.Windows["ignored"]
	assert.False(t, ok)
}

func TestState_NilInputsAreNoOps(t *testing.T) {
	t.Parallel()

	state := extui.NewState()
	state.ApplyLayout(nil)
	state.SetCursor(nil)
	state.ResetWindowOverride("")
	state.AppendDrawOp(nil)

	assert.Nil(t, state.Layout)
	assert.Nil(t, state.Cursor)
	assert.Empty(t, state.Overrides)
}

func TestCloneHelpersHandleNilInputs(t *testing.T) {
	t.Parallel()

	buffer := extui.CloneBuffer("empty", nil)
	assert.Equal(t, "empty", buffer.Name)
	assert.NotNil(t, buffer.Metadata)
	assert.NotNil(t, buffer.Blocks)
	assert.NotNil(t, buffer.Chars)
}
