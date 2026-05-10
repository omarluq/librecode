package terminal

import (
	"strings"

	"github.com/omarluq/librecode/internal/extension"
)

func (app *App) currentRuntimeLayout() runtimeLayout {
	width, height := app.screenSize()

	return app.currentRuntimeLayoutForSize(width, height)
}

func (app *App) currentRuntimeLayoutForSize(width, height int) runtimeLayout {
	layout := app.defaultRuntimeLayout(width, height)

	return app.mergeRuntimeLayout(&layout)
}

func (app *App) defaultRuntimeLayout(width, height int) runtimeLayout {
	statusLines := app.footerLines(width)
	autocompleteLines := app.autocompleteLines(width)
	maxComposerHeight := min(defaultEditorRows, max(3, height-len(statusLines)-len(autocompleteLines)-2))
	maxComposerHeight = max(3, maxComposerHeight)
	composerHeight := len(app.renderComposerEditor(width, max(1, maxComposerHeight-2)).Lines)
	reservedRows := len(statusLines) + len(autocompleteLines) + composerHeight
	if reservedRows > height {
		composerHeight = max(3, height-len(statusLines)-len(autocompleteLines))
		reservedRows = len(statusLines) + len(autocompleteLines) + composerHeight
	}
	transcriptHeight := max(0, height-reservedRows)
	autocompleteStart := transcriptHeight
	composerStart := autocompleteStart + len(autocompleteLines)
	statusStart := height - len(statusLines)

	return runtimeLayout{
		Windows: nil,
		Width:   width,
		Height:  height,
		Transcript: extension.WindowState{
			Metadata:  map[string]any{extensionMetadataCount: len(app.messages)},
			Name:      extensionBufferTranscript,
			Role:      extensionBufferTranscript,
			Buffer:    extensionBufferTranscript,
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
			Buffer:    extensionBufferStatus,
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
			Name:      extensionBufferComposer,
			Role:      extensionBufferComposer,
			Buffer:    extensionBufferComposer,
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
			Name:      extensionBufferStatus,
			Role:      extensionBufferStatus,
			Buffer:    extensionBufferStatus,
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

func (app *App) mergeRuntimeLayout(layout *runtimeLayout) runtimeLayout {
	windows := app.cloneRuntimeWindows(layout)
	transcript := windows[extensionBufferTranscript]
	autocomplete := windows["autocomplete"]
	composer := windows[extensionBufferComposer]
	status := windows[extensionBufferStatus]

	return runtimeLayout{
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
	window, ok := app.runtimeWindows[name]
	if !ok {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(window.Renderer), windowRendererExtension)
}

func (app *App) cloneRuntimeWindows(layout *runtimeLayout) map[string]extension.WindowState {
	windows := map[string]extension.WindowState{
		layout.Transcript.Name:   layout.Transcript,
		layout.Autocomplete.Name: layout.Autocomplete,
		layout.Composer.Name:     layout.Composer,
		layout.Status.Name:       layout.Status,
	}
	if app.runtimeLayout != nil && len(app.runtimeLayout.Windows) > 0 {
		for name := range app.runtimeLayout.Windows {
			windows[name] = app.runtimeLayout.Windows[name]
		}
	}
	for name := range app.runtimeWindows {
		windows[name] = app.runtimeWindows[name]
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
