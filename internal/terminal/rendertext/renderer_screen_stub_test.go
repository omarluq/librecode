package rendertext_test

import (
	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
)

type recordingScreen struct {
	cells map[[2]int]rune
}

func (screen *recordingScreen) Init() error            { return nil }
func (screen *recordingScreen) Fini()                  {}
func (screen *recordingScreen) Clear()                 {}
func (screen *recordingScreen) Fill(rune, tcell.Style) {}
func (screen *recordingScreen) Put(_, _ int, value string, _ tcell.Style) (remaining string, width int) {
	return value, 0
}
func (screen *recordingScreen) PutStr(_, _ int, _ string)                      {}
func (screen *recordingScreen) PutStrStyled(_, _ int, _ string, _ tcell.Style) {}
func (screen *recordingScreen) Get(_, _ int) (content string, style tcell.Style, width int) {
	return "", tcell.StyleDefault, 0
}
func (screen *recordingScreen) SetContent(x, y int, primary rune, _ []rune, _ tcell.Style) {
	screen.cells[[2]int{x, y}] = primary
}
func (screen *recordingScreen) SetStyle(tcell.Style)                                 {}
func (screen *recordingScreen) ShowCursor(_, _ int)                                  {}
func (screen *recordingScreen) HideCursor()                                          {}
func (screen *recordingScreen) SetCursorStyle(tcell.CursorStyle, ...cellcolor.Color) {}
func (screen *recordingScreen) Size() (width, height int)                            { return 0, 0 }
func (screen *recordingScreen) EventQ() chan tcell.Event                             { return nil }
func (screen *recordingScreen) EnableMouse(...tcell.MouseFlags)                      {}
func (screen *recordingScreen) DisableMouse()                                        {}
func (screen *recordingScreen) EnablePaste()                                         {}
func (screen *recordingScreen) DisablePaste()                                        {}
func (screen *recordingScreen) EnableFocus()                                         {}
func (screen *recordingScreen) DisableFocus()                                        {}
func (screen *recordingScreen) Colors() int                                          { return 0 }
func (screen *recordingScreen) Show()                                                {}
func (screen *recordingScreen) Sync()                                                {}
func (screen *recordingScreen) CharacterSet() string                                 { return "UTF-8" }
func (screen *recordingScreen) RegisterRuneFallback(rune, string)                    {}
func (screen *recordingScreen) UnregisterRuneFallback(rune)                          {}
func (screen *recordingScreen) Resize(int, int, int, int)                            {}
func (screen *recordingScreen) Suspend() error                                       { return nil }
func (screen *recordingScreen) Resume() error                                        { return nil }
func (screen *recordingScreen) Beep() error                                          { return nil }
func (screen *recordingScreen) SetSize(int, int)                                     {}
func (screen *recordingScreen) LockRegion(int, int, int, int, bool)                  {}
func (screen *recordingScreen) SetTitle(string)                                      {}
func (screen *recordingScreen) SetClipboard([]byte)                                  {}
func (screen *recordingScreen) GetClipboard()                                        {}
func (screen *recordingScreen) HasClipboard() bool                                   { return false }
func (screen *recordingScreen) ShowNotification(string, string)                      {}
func (screen *recordingScreen) KeyboardProtocol() tcell.KeyProtocol                  { return 0 }
func (screen *recordingScreen) Terminal() (name, version string)                     { return "", "" }
