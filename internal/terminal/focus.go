package terminal

import "github.com/omarluq/librecode/internal/extension"

const (
	focusKindAutocomplete = "autocomplete"
	focusKindComposer     = "composer"
	focusKindPanel        = "panel"
)

func (app *App) focusState() extension.FocusState {
	if app.mode == modePanel && app.panel != nil {
		return extension.FocusState{
			Kind:      focusKindPanel,
			Window:    extensionBufferTranscript,
			Buffer:    extensionBufferTranscript,
			Role:      string(app.selectedPanelKind),
			PanelKind: string(app.selectedPanelKind),
			Exclusive: true,
		}
	}
	if app.autocompleteActive() {
		return extension.FocusState{
			Kind:      focusKindAutocomplete,
			Window:    focusKindAutocomplete,
			Buffer:    extensionBufferStatus,
			Role:      focusKindAutocomplete,
			PanelKind: "",
			Exclusive: true,
		}
	}

	return extension.FocusState{
		Kind:      focusKindComposer,
		Window:    extensionBufferComposer,
		Buffer:    extensionBufferComposer,
		Role:      extensionBufferComposer,
		PanelKind: "",
		Exclusive: false,
	}
}
