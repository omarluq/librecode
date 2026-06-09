package terminal

import (
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal/extui"
)

const (
	focusKindAutocomplete = "autocomplete"
	focusKindComposer     = "composer"
	focusKindPanel        = "panel"
)

func (app *App) focusState() extension.FocusState {
	if app.mode == modePanel && app.panel != nil {
		return extension.FocusState{
			Kind:      focusKindPanel,
			Window:    extui.BufferTranscript,
			Buffer:    extui.BufferTranscript,
			Role:      string(app.selectedPanelKind),
			PanelKind: string(app.selectedPanelKind),
			Exclusive: true,
		}
	}
	if app.autocompleteActive() {
		return extension.FocusState{
			Kind:      focusKindAutocomplete,
			Window:    focusKindAutocomplete,
			Buffer:    extui.BufferStatus,
			Role:      focusKindAutocomplete,
			PanelKind: "",
			Exclusive: true,
		}
	}

	return extension.FocusState{
		Kind:      focusKindComposer,
		Window:    extui.BufferComposer,
		Buffer:    extui.BufferComposer,
		Role:      extui.BufferComposer,
		PanelKind: "",
		Exclusive: false,
	}
}
