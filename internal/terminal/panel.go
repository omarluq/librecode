package terminal

import (
	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tui"
)

const (
	panelModel        panel.Kind = "model"
	panelScopedModels panel.Kind = "scoped_models"
	panelAuthLogin    panel.Kind = "auth_login"
	panelAuthLogout   panel.Kind = "auth_logout"
	panelSettings     panel.Kind = "settings"
	panelHotkeys      panel.Kind = hotkeysCommandName
	panelChangelog    panel.Kind = changelogCommandName
	panelSessions     panel.Kind = "sessions"
	panelTree         panel.Kind = "tree"

	hotkeysCommandName   = "hotkeys"
	changelogCommandName = "changelog"
)

func panelRenderOptions(width, height int, theme terminalTheme, bindings *keybindings) tui.ListRenderOptions {
	return tui.ListRenderOptions{
		Styles: tui.ListStyles{
			Border:   theme.style(colorBorder),
			Accent:   theme.style(colorAccent),
			Muted:    theme.style(colorMuted),
			Text:     theme.style(colorText),
			Selected: theme.selected(),
			Dim:      theme.style(colorDim),
		},
		Hints: tui.ListHints{
			Up:      bindings.hint(actionSelectUp),
			Down:    bindings.hint(actionSelectDown),
			Confirm: bindings.hint(actionSelectConfirm),
			Cancel:  bindings.hint(actionSelectCancel),
		},
		Width:  width,
		Height: height,
	}
}
