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

var piThemeColors = []themeColor{
	{token: colorAccent, dark: hexColor(0x8abeb7), light: hexColor(0x0066cc)},
	{token: colorBorder, dark: hexColor(0x5f87ff), light: hexColor(0x0066cc)},
	{token: colorBorderAccent, dark: hexColor(0x00d7ff), light: hexColor(0x0088cc)},
	{token: colorBorderMuted, dark: hexColor(0x505050), light: hexColor(0x999999)},
	{token: colorSuccess, dark: hexColor(0xb5bd68), light: hexColor(0x22863a)},
	{token: colorError, dark: hexColor(0xcc6666), light: hexColor(0xcb2431)},
	{token: colorWarning, dark: hexColor(0xffff00), light: hexColor(0xb08800)},
	{token: colorMuted, dark: hexColor(0x808080), light: hexColor(0x666666)},
	{token: colorDim, dark: hexColor(0x666666), light: hexColor(0x999999)},
	{token: colorText, dark: cellcolor.Default, light: cellcolor.Black},
	{token: colorCodeBg, dark: hexColor(0x1f2330), light: hexColor(0xf6f8fa)},
	{token: colorCodeText, dark: hexColor(0xd7d7d7), light: hexColor(0x24292f)},
	{token: colorDiffAdd, dark: hexColor(0xb5bd68), light: hexColor(0x22863a)},
	{token: colorDiffDelete, dark: hexColor(0xcc6666), light: hexColor(0xcb2431)},
	{token: colorThinkingText, dark: hexColor(0x808080), light: hexColor(0x666666)},
	{token: colorSelectedBg, dark: hexColor(0x3a3a4a), light: hexColor(0xe8f0fe)},
	{token: colorUserMessageBg, dark: hexColor(0x343541), light: hexColor(0xf3f4f6)},
	{token: colorCustomMessageBg, dark: hexColor(0x2d2838), light: hexColor(0xf3efff)},
	{token: colorToolPendingBg, dark: hexColor(0x282832), light: hexColor(0xf4f4ff)},
	{token: colorToolSuccessBg, dark: hexColor(0x283228), light: hexColor(0xf0fff4)},
	{token: colorToolErrorBg, dark: hexColor(0x3c2828), light: hexColor(0xfff0f0)},
	{token: colorBashMode, dark: hexColor(0xb5bd68), light: hexColor(0x22863a)},
}

func darkTheme() terminalTheme {
	return newTheme("dark", false)
}

func lightTheme() terminalTheme {
	return newTheme("light", true)
}

func newTheme(name string, light bool) terminalTheme {
	colors := make(map[colorToken]tcell.Color, len(piThemeColors))
	for _, spec := range piThemeColors {
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

func hexColor(value int32) tcell.Color {
	return cellcolor.NewHexColor(value)
}
