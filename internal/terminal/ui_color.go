package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"
)

func (app *App) namedUIColor(name string) tcell.Color {
	trimmed := strings.TrimSpace(name)
	if color, ok := app.theme.colors[colorToken(trimmed)]; ok {
		return color
	}

	return app.namedUIColorAlias(trimmed)
}

func (app *App) namedUIColorAlias(name string) tcell.Color {
	switch strings.ToLower(name) {
	case "default", "white":
		return app.theme.colors[colorText]
	default:
		return tcell.ColorDefault
	}
}
