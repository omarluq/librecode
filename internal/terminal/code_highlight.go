package terminal

import "github.com/omarluq/librecode/internal/tui"

func codeTheme(theme terminalTheme) tui.CodeTheme {
	return tui.CodeTheme{
		Text:    theme.colors[colorCodeText],
		Accent:  theme.colors[colorAccent],
		Success: theme.colors[colorSuccess],
		Warning: theme.colors[colorWarning],
		Dim:     theme.colors[colorDim],
		Muted:   theme.colors[colorMuted],
		DiffAdd: theme.colors[colorDiffAdd],
		DiffDel: theme.colors[colorDiffDelete],
	}
}
