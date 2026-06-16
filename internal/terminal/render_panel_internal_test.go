package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	renderPanelFirstItem  = "one"
	renderPanelSecondItem = "two"
)

func TestDrawPanelUsesComposerReserveAndReturnsNextRow(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.panel = panel.New(panelModel, "Pick", "", []tui.ListItem{
		{Value: renderPanelFirstItem, Title: renderPanelFirstItem, Description: "", Meta: ""},
		{Value: renderPanelSecondItem, Title: renderPanelSecondItem, Description: "", Meta: ""},
	}, false)
	app.frame = tui.NewCellBuffer(30, 10, app.theme.style(colorText))

	nextRow := app.drawPanel(30, 10, 1)

	assert.Greater(t, nextRow, 1)
	assert.Contains(t, frameText(app.frame), renderPanelFirstItem)
}

func TestDrawPanelWindowSkipsInvisibleOwnedAndEmptyWindows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		renderer string
		visible  bool
		owned    bool
		height   int
	}{
		{name: "invisible", renderer: "", visible: false, owned: false, height: 5},
		{name: "empty height", renderer: "", visible: true, owned: false, height: 0},
		{name: "extension owned", renderer: windowRendererExtension, visible: true, owned: true, height: 5},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			window := testPanelWindow(testCase.renderer, testCase.visible, 0, 0, 30, testCase.height)
			app.panel = panel.New(panelModel, "Pick", "", []tui.ListItem{testPanelListItem()}, false)
			app.frame = tui.NewCellBuffer(30, 6, app.theme.style(colorText))

			layout := testPanelLayout(&window)
			if testCase.owned {
				app.extensionUI.Windows[window.Name] = window
			}

			app.drawPanelWindow(layout)

			assert.NotContains(t, frameText(app.frame), "Pick")
		})
	}
}

func TestDrawPanelWindowRendersIntoTranscriptWindow(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.panel = panel.New(panelModel, "Pick", "", []tui.ListItem{testPanelListItem()}, false)
	app.frame = tui.NewCellBuffer(30, 6, app.theme.style(colorText))
	window := testPanelWindow("", true, 0, 2, 30, 4)
	layout := testPanelLayout(&window)

	app.drawPanelWindow(layout)

	assert.Contains(t, frameText(app.frame), renderPanelFirstItem)
}

func testPanelListItem() tui.ListItem {
	return tui.ListItem{
		Value:       renderPanelFirstItem,
		Title:       renderPanelFirstItem,
		Description: "",
		Meta:        "",
	}
}

func testPanelLayout(transcript *extension.WindowState) *extui.Layout {
	return &extui.Layout{
		Windows:      nil,
		Transcript:   *transcript,
		Autocomplete: emptyPanelWindow(),
		Composer:     emptyPanelWindow(),
		Status:       emptyPanelWindow(),
		Width:        0,
		Height:       0,
	}
}

func emptyPanelWindow() extension.WindowState {
	return testPanelWindow("", false, 0, 0, 0, 0)
}

func testPanelWindow(renderer string, visible bool, column, row, width, height int) extension.WindowState {
	return extension.WindowState{
		Metadata:  nil,
		Name:      extui.BufferTranscript,
		Role:      "",
		Buffer:    "",
		Renderer:  renderer,
		X:         column,
		Y:         row,
		Width:     width,
		Height:    height,
		CursorRow: 0,
		CursorCol: 0,
		Visible:   visible,
	}
}
