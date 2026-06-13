package terminal

import (
	"strings"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
)

func (app *App) currentRuntimeLayout() extui.Layout {
	width, height := app.screenSize()

	return app.currentRuntimeLayoutForSize(width, height)
}

func (app *App) currentRuntimeLayoutForSize(width, height int) extui.Layout {
	layout := app.defaultRuntimeLayout(width, height)

	return app.mergeRuntimeLayout(&layout)
}

func (app *App) defaultRuntimeLayout(width, height int) extui.Layout {
	statusLines := app.footerLines(width)
	autocompleteLines := app.autocompleteLines(width)
	availableComposerRows := height - len(statusLines) - len(autocompleteLines) - composerBorderRows
	maxComposerHeight := min(defaultEditorRows, max(minimumComposerHeight, availableComposerRows))
	maxComposerHeight = max(minimumComposerHeight, maxComposerHeight)
	composerHeight := len(app.renderComposerEditor(width, max(1, maxComposerHeight-composerBorderRows)).Lines)

	reservedRows := len(statusLines) + len(autocompleteLines) + composerHeight
	if reservedRows > height {
		composerHeight = max(minimumComposerHeight, height-len(statusLines)-len(autocompleteLines))
		reservedRows = len(statusLines) + len(autocompleteLines) + composerHeight
	}

	transcriptHeight := max(0, height-reservedRows)
	autocompleteStart := transcriptHeight
	composerStart := autocompleteStart + len(autocompleteLines)
	statusStart := height - len(statusLines)

	return extui.Layout{
		Windows: nil,
		Width:   width,
		Height:  height,
		Transcript: extension.WindowState{
			Metadata:  map[string]any{extui.MetadataCount: len(app.transcript.History)},
			Name:      extui.BufferTranscript,
			Role:      extui.BufferTranscript,
			Buffer:    extui.BufferTranscript,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         0,
			Width:     width,
			Height:    transcriptHeight,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
		Autocomplete: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      "autocomplete",
			Role:      "autocomplete",
			Buffer:    extui.BufferStatus,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         autocompleteStart,
			Width:     width,
			Height:    len(autocompleteLines),
			CursorRow: 0,
			CursorCol: 0,
			Visible:   len(autocompleteLines) > 0,
		},
		Composer: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      extui.BufferComposer,
			Role:      extui.BufferComposer,
			Buffer:    extui.BufferComposer,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         composerStart,
			Width:     width,
			Height:    composerHeight,
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
		Status: extension.WindowState{
			Metadata:  map[string]any{},
			Name:      extui.BufferStatus,
			Role:      extui.BufferStatus,
			Buffer:    extui.BufferStatus,
			Renderer:  windowRendererDefault,
			X:         0,
			Y:         statusStart,
			Width:     width,
			Height:    len(statusLines),
			CursorRow: 0,
			CursorCol: 0,
			Visible:   true,
		},
	}
}

func (app *App) mergeRuntimeLayout(layout *extui.Layout) extui.Layout {
	windows := app.cloneRuntimeWindows(layout)
	transcript := windows[extui.BufferTranscript]
	autocomplete := windows["autocomplete"]
	composer := windows[extui.BufferComposer]
	status := windows[extui.BufferStatus]

	return extui.Layout{
		Width:        layout.Width,
		Height:       layout.Height,
		Transcript:   transcript,
		Autocomplete: autocomplete,
		Composer:     composer,
		Status:       status,
		Windows:      windows,
	}
}

func (app *App) extensionOwnsWindow(name string) bool {
	window, ok := app.extensionUI.Windows[name]
	if !ok {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(window.Renderer), windowRendererExtension)
}

func (app *App) cloneRuntimeWindows(layout *extui.Layout) map[string]extension.WindowState {
	windows := map[string]extension.WindowState{
		layout.Transcript.Name:   layout.Transcript,
		layout.Autocomplete.Name: layout.Autocomplete,
		layout.Composer.Name:     layout.Composer,
		layout.Status.Name:       layout.Status,
	}

	if app.extensionUI.Layout != nil && len(app.extensionUI.Layout.Windows) > 0 {
		for name := range app.extensionUI.Layout.Windows {
			windows[name] = app.extensionUI.Layout.Windows[name]
		}
	}

	for name := range app.extensionUI.Windows {
		windows[name] = app.extensionUI.Windows[name]
	}

	for name := range windows {
		window := windows[name]
		if window.Name == "" {
			window.Name = name
		}

		windows[name] = window
	}

	return windows
}
