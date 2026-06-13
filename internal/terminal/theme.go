package terminal

import (
	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
)

type colorToken string

const (
	themeNameLight = "light"
)

const (
	colorDarkSuccess  = "#b5bd68"
	colorLightSuccess = "#22863a"
	colorLightMuted   = "#666666"
)

const (
	colorAccent          colorToken = "accent"
	colorBorder          colorToken = "border"
	colorBorderAccent    colorToken = "borderAccent"
	colorBorderMuted     colorToken = "borderMuted"
	colorSuccess         colorToken = "success"
	colorError           colorToken = "error"
	colorWarning         colorToken = "warning"
	colorMuted           colorToken = "muted"
	colorDim             colorToken = "dim"
	colorText            colorToken = "text"
	colorCodeBg          colorToken = "codeBg"
	colorCodeText        colorToken = "codeText"
	colorDiffAdd         colorToken = "diffAdd"
	colorDiffDelete      colorToken = "diffDelete"
	colorThinkingText    colorToken = "thinkingText"
	colorSelectedBg      colorToken = "selectedBg"
	colorUserMessageBg   colorToken = "userMessageBg"
	colorCustomMessageBg colorToken = "customMessageBg"
	colorToolPendingBg   colorToken = "toolPendingBg"
	colorToolSuccessBg   colorToken = "toolSuccessBg"
	colorToolErrorBg     colorToken = "toolErrorBg"
	colorBashMode        colorToken = "bashMode"
)

type terminalTheme struct {
	colors map[colorToken]tcell.Color
	name   string
}

type themeColor struct {
	token colorToken
	dark  tcell.Color
	light tcell.Color
}

func piThemeColors() []themeColor {
	return []themeColor{
		{token: colorAccent, dark: hexColorFromString("#8abeb7"), light: hexColorFromString("#0066cc")},
		{token: colorBorder, dark: hexColorFromString("#5f87ff"), light: hexColorFromString("#0066cc")},
		{token: colorBorderAccent, dark: hexColorFromString("#00d7ff"), light: hexColorFromString("#0088cc")},
		{token: colorBorderMuted, dark: hexColorFromString("#505050"), light: hexColorFromString("#999999")},
		{
			token: colorSuccess,
			dark:  hexColorFromString(colorDarkSuccess),
			light: hexColorFromString(colorLightSuccess),
		},
		{token: colorError, dark: hexColorFromString("#cc6666"), light: hexColorFromString("#cb2431")},
		{token: colorWarning, dark: hexColorFromString("#ffff00"), light: hexColorFromString("#b08800")},
		{token: colorMuted, dark: hexColorFromString("#808080"), light: hexColorFromString(colorLightMuted)},
		{token: colorDim, dark: hexColorFromString(colorLightMuted), light: hexColorFromString("#999999")},
		{token: colorText, dark: cellcolor.Default, light: cellcolor.Black},
		{token: colorCodeBg, dark: hexColorFromString("#1f2330"), light: hexColorFromString("#f6f8fa")},
		{token: colorCodeText, dark: hexColorFromString("#d7d7d7"), light: hexColorFromString("#24292f")},
		{
			token: colorDiffAdd,
			dark:  hexColorFromString(colorDarkSuccess),
			light: hexColorFromString(colorLightSuccess),
		},
		{token: colorDiffDelete, dark: hexColorFromString("#cc6666"), light: hexColorFromString("#cb2431")},
		{token: colorThinkingText, dark: hexColorFromString("#808080"), light: hexColorFromString(colorLightMuted)},
		{token: colorSelectedBg, dark: hexColorFromString("#3a3a4a"), light: hexColorFromString("#e8f0fe")},
		{token: colorUserMessageBg, dark: hexColorFromString("#343541"), light: hexColorFromString("#f3f4f6")},
		{token: colorCustomMessageBg, dark: hexColorFromString("#2d2838"), light: hexColorFromString("#f3efff")},
		{token: colorToolPendingBg, dark: hexColorFromString("#282832"), light: hexColorFromString("#f4f4ff")},
		{token: colorToolSuccessBg, dark: hexColorFromString("#283228"), light: hexColorFromString("#f0fff4")},
		{token: colorToolErrorBg, dark: hexColorFromString("#3c2828"), light: hexColorFromString("#fff0f0")},
		{
			token: colorBashMode,
			dark:  hexColorFromString(colorDarkSuccess),
			light: hexColorFromString(colorLightSuccess),
		},
	}
}

func darkTheme() terminalTheme {
	return newTheme("dark", false)
}

func lightTheme() terminalTheme {
	return newTheme("light", true)
}

func newTheme(name string, light bool) terminalTheme {
	colorSpecs := piThemeColors()

	colors := make(map[colorToken]tcell.Color, len(colorSpecs))
	for _, spec := range colorSpecs {
		colors[spec.token] = spec.dark
		if light {
			colors[spec.token] = spec.light
		}
	}

	return terminalTheme{colors: colors, name: name}
}

func themeByName(name string) terminalTheme {
	if name == themeNameLight {
		return lightTheme()
	}

	return darkTheme()
}

func (theme terminalTheme) style(token colorToken) tcell.Style {
	color, ok := theme.colors[token]
	if !ok {
		color = tcell.ColorDefault
	}

	return tcell.StyleDefault.Foreground(color)
}

func (theme terminalTheme) background(token colorToken) tcell.Style {
	color, ok := theme.colors[token]
	if !ok {
		color = tcell.ColorDefault
	}

	return tcell.StyleDefault.Foreground(theme.colors[colorText]).Background(color)
}

func (theme terminalTheme) selected() tcell.Style {
	return theme.background(colorSelectedBg).Foreground(theme.colors[colorAccent]).Bold(true)
}

func hexColorFromString(value string) tcell.Color {
	return cellcolor.GetColor(value)
}
