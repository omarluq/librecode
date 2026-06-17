package terminal

import (
	"errors"
	"sync"
	"testing"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/tui"
)

const (
	clipboardTestTerminal = "clipboard-test"
	clipboardCopyText     = "copy me"
	clipboardWorldText    = "world"
)

type fakeSystemClipboard struct {
	err    error
	writes []string
}

func newFakeSystemClipboard() *fakeSystemClipboard {
	return &fakeSystemClipboard{err: nil, writes: nil}
}

func (clipboard *fakeSystemClipboard) WriteText(text string) error {
	clipboard.writes = append(clipboard.writes, text)

	return clipboard.err
}

type clipboardScreen struct {
	cells     *tcell.CellBuffer
	content   map[[2]int]tui.Cell
	events    chan tcell.Event
	stop      chan struct{}
	clipboard []byte
	mu        sync.Mutex
	size      [2]int
}

func newClipboardScreen() *clipboardScreen {
	return &clipboardScreen{
		cells:     newTcellBuffer(80, 24),
		content:   map[[2]int]tui.Cell{},
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

func (screen *clipboardScreen) SetContent(x, y int, primary rune, combiner []rune, style tcell.Style) {
	screen.mu.Lock()
	defer screen.mu.Unlock()

	screen.content[[2]int{x, y}] = tui.Cell{Style: style, Comb: append([]rune(nil), combiner...), Rune: primary}
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
func (screen *clipboardScreen) SetClipboard(data []byte) {
	screen.clipboard = append(screen.clipboard[:0], data...)
}
func (screen *clipboardScreen) GetClipboard()                   {}
func (screen *clipboardScreen) HasClipboard() bool              { return true }
func (screen *clipboardScreen) ShowNotification(string, string) {}
func (screen *clipboardScreen) KeyboardProtocol() tcell.KeyProtocol {
	return tcell.LegacyKeyboard
}
func (screen *clipboardScreen) Terminal() (name, version string) {
	return clipboardTestTerminal, clipboardTestTerminal
}
func (screen *clipboardScreen) Lock()                               { screen.mu.Lock() }
func (screen *clipboardScreen) Unlock()                             { screen.mu.Unlock() }
func (screen *clipboardScreen) GetCells() *tcell.CellBuffer         { return screen.cells }
func (screen *clipboardScreen) StopQ() <-chan struct{}              { return screen.stop }
func (screen *clipboardScreen) LockRegion(int, int, int, int, bool) {}

func TestCopyTextToClipboardWritesScreenAndSystemClipboards(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	system := newFakeSystemClipboard()

	copyTextToClipboard(screen, system, clipboardCopyText)

	assert.Equal(t, clipboardCopyText, string(screen.clipboard))
	assert.Equal(t, []string{clipboardCopyText}, system.writes)
}

func TestCopyTextToClipboardIgnoresSystemClipboardErrors(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	system := &fakeSystemClipboard{err: errors.New("clipboard unavailable"), writes: nil}

	copyTextToClipboard(screen, system, clipboardCopyText)

	assert.Equal(t, clipboardCopyText, string(screen.clipboard))
	assert.Equal(t, []string{clipboardCopyText}, system.writes)
}

func TestCopyTextToClipboardHandlesMissingSystemClipboard(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()

	copyTextToClipboard(screen, nil, clipboardCopyText)

	assert.Equal(t, clipboardCopyText, string(screen.clipboard))
}

func TestCopyTextToClipboardIgnoresEmptyText(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	system := newFakeSystemClipboard()

	copyTextToClipboard(screen, system, "")

	assert.Empty(t, screen.clipboard)
	assert.Empty(t, system.writes)
}

func TestCopyTextToClipboardHandlesMissingScreen(t *testing.T) {
	t.Parallel()

	system := newFakeSystemClipboard()

	copyTextToClipboard(nil, system, clipboardCopyText)

	assert.Empty(t, system.writes)
}
