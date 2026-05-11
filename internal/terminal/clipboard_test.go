//nolint:testpackage // These tests exercise unexported terminal selection helpers.
package terminal

import (
	"sync"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
)

const clipboardTestTerminal = "clipboard-test"

type clipboardScreen struct {
	cells     *tcell.CellBuffer
	content   map[[2]int]screenCell
	events    chan tcell.Event
	stop      chan struct{}
	clipboard []byte
	mu        sync.Mutex
	size      [2]int
}

func newClipboardScreen() *clipboardScreen {
	return &clipboardScreen{
		cells:     newTcellBuffer(80, 24),
		content:   map[[2]int]screenCell{},
		events:    make(chan tcell.Event, 8),
		stop:      make(chan struct{}),
		clipboard: nil,
		mu:        sync.Mutex{},
		size:      [2]int{80, 24},
	}
}

func newTcellBuffer(width, height int) *tcell.CellBuffer {
	var buffer tcell.CellBuffer
	buffer.Resize(width, height)

	return &buffer
}

func (screen *clipboardScreen) Init() error            { return nil }
func (screen *clipboardScreen) Fini()                  {}
func (screen *clipboardScreen) Clear()                 {}
func (screen *clipboardScreen) Fill(rune, tcell.Style) {}

func (screen *clipboardScreen) Put(_, _ int, _ string, _ tcell.Style) (remaining string, width int) {
	return "", 0
}

func (screen *clipboardScreen) PutStr(int, int, string)                    {}
func (screen *clipboardScreen) PutStrStyled(int, int, string, tcell.Style) {}

func (screen *clipboardScreen) Get(_, _ int) (text string, style tcell.Style, width int) {
	return "", tcell.StyleDefault, 1
}

func (screen *clipboardScreen) SetContent(x, y int, primary rune, _ []rune, style tcell.Style) {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	screen.content[[2]int{x, y}] = screenCell{Style: style, Rune: primary}
}

func (screen *clipboardScreen) SetStyle(tcell.Style)                                 {}
func (screen *clipboardScreen) ShowCursor(int, int)                                  {}
func (screen *clipboardScreen) HideCursor()                                          {}
func (screen *clipboardScreen) SetCursorStyle(tcell.CursorStyle, ...cellcolor.Color) {}

func (screen *clipboardScreen) Size() (width, height int) {
	return screen.size[0], screen.size[1]
}

func (screen *clipboardScreen) EventQ() chan tcell.Event          { return screen.events }
func (screen *clipboardScreen) EnableMouse(...tcell.MouseFlags)   {}
func (screen *clipboardScreen) DisableMouse()                     {}
func (screen *clipboardScreen) EnablePaste()                      {}
func (screen *clipboardScreen) DisablePaste()                     {}
func (screen *clipboardScreen) EnableFocus()                      {}
func (screen *clipboardScreen) DisableFocus()                     {}
func (screen *clipboardScreen) Colors() int                       { return 256 }
func (screen *clipboardScreen) Show()                             {}
func (screen *clipboardScreen) Sync()                             {}
func (screen *clipboardScreen) CharacterSet() string              { return "UTF-8" }
func (screen *clipboardScreen) RegisterRuneFallback(rune, string) {}
func (screen *clipboardScreen) UnregisterRuneFallback(rune)       {}
func (screen *clipboardScreen) Resize(int, int, int, int)         {}
func (screen *clipboardScreen) Suspend() error                    { return nil }
func (screen *clipboardScreen) Resume() error                     { return nil }
func (screen *clipboardScreen) Beep() error                       { return nil }
func (screen *clipboardScreen) SetSize(width, height int)         { screen.size = [2]int{width, height} }
func (screen *clipboardScreen) SetTitle(string)                   {}
func (screen *clipboardScreen) Tty() (tcell.Tty, bool)            { return nil, false }
func (screen *clipboardScreen) SetClipboard(data []byte) {
	screen.clipboard = append(screen.clipboard[:0], data...)
}
func (screen *clipboardScreen) GetClipboard()                   {}
func (screen *clipboardScreen) HasClipboard() bool              { return true }
func (screen *clipboardScreen) ShowNotification(string, string) {}
func (screen *clipboardScreen) Terminal() (name, version string) {
	return clipboardTestTerminal, clipboardTestTerminal
}
func (screen *clipboardScreen) Lock()                               { screen.mu.Lock() }
func (screen *clipboardScreen) Unlock()                             { screen.mu.Unlock() }
func (screen *clipboardScreen) GetCells() *tcell.CellBuffer         { return screen.cells }
func (screen *clipboardScreen) StopQ() <-chan struct{}              { return screen.stop }
func (screen *clipboardScreen) LockRegion(int, int, int, int, bool) {}
