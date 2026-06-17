package terminal

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v3"
	cellcolor "github.com/gdamore/tcell/v3/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tui"
	"golang.design/x/clipboard"
)

const (
	clipboardTestTerminal = "clipboard-test"
	clipboardCopyText     = "copy me"
	clipboardWorldText    = "world"
)

type mockFileInfo struct {
	mock.Mock
}

func newMockFileInfo(t *testing.T, mode fs.FileMode) *mockFileInfo {
	t.Helper()

	info := &mockFileInfo{Mock: mock.Mock{}}
	info.On("Mode").Return(mode).Once()

	return info
}

func (info *mockFileInfo) Name() string { return "" }
func (info *mockFileInfo) Size() int64  { return 0 }
func (info *mockFileInfo) Mode() fs.FileMode {
	mode, ok := info.Called().Get(0).(fs.FileMode)
	if !ok {
		panic("mock file info mode has unexpected type")
	}

	return mode
}
func (info *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (info *mockFileInfo) IsDir() bool        { return false }
func (info *mockFileInfo) Sys() any           { return nil }

type fakeSystemClipboard struct {
	mock.Mock
}

func newFakeSystemClipboard() *fakeSystemClipboard {
	return &fakeSystemClipboard{Mock: mock.Mock{}}
}

func (writer *fakeSystemClipboard) WriteText(text string) error {
	args := writer.Called(text)
	if err := args.Error(0); err != nil {
		return fmt.Errorf("fake system clipboard write: %w", err)
	}

	return nil
}

func expectClipboardWrite(t *testing.T, writer *fakeSystemClipboard, text string) {
	t.Helper()

	writer.On("WriteText", text).Return(nil).Once()
}

func assertClipboardExpectations(t *testing.T, writer *fakeSystemClipboard) {
	t.Helper()

	writer.AssertExpectations(t)
}

func assertNoClipboardWrite(t *testing.T, writer *fakeSystemClipboard) {
	t.Helper()

	writer.AssertNotCalled(t, "WriteText", mock.Anything)
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

func TestCandidateWaylandDisplay(t *testing.T) {
	t.Parallel()

	const runtimeDir = "/run/user/1000"

	files := map[string]fs.FileMode{
		filepath.Join(runtimeDir, "wayland-0"):      fs.ModeSocket,
		filepath.Join(runtimeDir, "wayland-0.lock"): 0,
		filepath.Join(runtimeDir, "wayland-1"):      0,
	}
	tests := []struct {
		name    string
		want    string
		matches []string
	}{
		{
			name: "uses first socket display",
			matches: []string{
				filepath.Join(runtimeDir, "wayland-0"),
				filepath.Join(runtimeDir, "wayland-0.lock"),
			},
			want: "wayland-0",
		},
		{
			name: "skips lock and regular files",
			matches: []string{
				filepath.Join(runtimeDir, "wayland-0.lock"),
				filepath.Join(runtimeDir, "wayland-1"),
			},
			want: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statCalls := make([]*mockFileInfo, 0, len(testCase.matches))
			got := candidateWaylandDisplay(
				runtimeDir,
				func(string) ([]string, error) { return testCase.matches, nil },
				func(path string) (fs.FileInfo, error) {
					mode, ok := files[path]
					if !ok {
						return nil, fs.ErrNotExist
					}

					info := newMockFileInfo(t, mode)
					statCalls = append(statCalls, info)

					return info, nil
				},
			)

			assert.Equal(t, testCase.want, got)

			for _, info := range statCalls {
				info.AssertExpectations(t)
			}
		})
	}
}

func TestDesktopClipboardWritesText(t *testing.T) {
	t.Parallel()

	changed := make(chan struct{})
	result := callDesktopClipboard(clipboardCopyText, nil, changed)

	require.NoError(t, result.err)
	assert.True(t, result.prepared)
	assert.True(t, result.initialized)
	require.Len(t, result.writes, 1)
	assert.Equal(t, clipboardCopyText, string(result.writes[0]))
}

func TestDesktopClipboardReturnsWriteFailure(t *testing.T) {
	t.Parallel()

	result := callDesktopClipboard(clipboardCopyText, nil, nil)

	require.Error(t, result.err)
	require.ErrorIs(t, result.err, errSystemClipboardWriteFailed)
	assert.True(t, result.prepared)
	assert.True(t, result.initialized)
	require.Len(t, result.writes, 1)
	assert.Equal(t, clipboardCopyText, string(result.writes[0]))
}

func TestDesktopClipboardReturnsInitError(t *testing.T) {
	t.Parallel()

	initErr := errors.New("init failed")
	result := callDesktopClipboard(clipboardCopyText, initErr, nil)

	require.Error(t, result.err)
	require.ErrorIs(t, result.err, initErr)
	assert.True(t, result.prepared)
	assert.True(t, result.initialized)
	assert.Empty(t, result.writes)
}

func TestDesktopClipboardIgnoresEmptyText(t *testing.T) {
	t.Parallel()

	result := callDesktopClipboard("", nil, make(chan struct{}))

	require.NoError(t, result.err)
	assert.False(t, result.prepared)
	assert.False(t, result.initialized)
	assert.Empty(t, result.writes)
}

type desktopClipboardCallResult struct {
	err         error
	writes      [][]byte
	prepared    bool
	initialized bool
}

func callDesktopClipboard(text string, initErr error, changed <-chan struct{}) desktopClipboardCallResult {
	result := desktopClipboardCallResult{err: nil, writes: make([][]byte, 0, 1), prepared: false, initialized: false}
	writer := desktopClipboard{
		prepare: func() {
			result.prepared = true
		},
		init: func() error {
			result.initialized = true

			return initErr
		},
		write: func(_ clipboard.Format, data []byte) <-chan struct{} {
			result.writes = append(result.writes, append([]byte(nil), data...))

			return changed
		},
	}

	result.err = writer.WriteText(text)

	return result
}

func TestCopyTextToClipboardWritesScreenAndSystemClipboards(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	system := newFakeSystemClipboard()
	expectClipboardWrite(t, system, clipboardCopyText)

	require.NoError(t, copyTextToClipboard(screen, system, clipboardCopyText))

	assert.Equal(t, clipboardCopyText, string(screen.clipboard))
	assertClipboardExpectations(t, system)
}

func TestCopyTextToClipboardReturnsSystemClipboardErrors(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	system := newFakeSystemClipboard()
	systemErr := errors.New("clipboard unavailable")
	system.On("WriteText", clipboardCopyText).Return(systemErr).Once()

	err := copyTextToClipboard(screen, system, clipboardCopyText)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write system clipboard")
	assert.Equal(t, clipboardCopyText, string(screen.clipboard))
	assertClipboardExpectations(t, system)
}

func TestCopyTextToClipboardHandlesMissingSystemClipboard(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()

	require.NoError(t, copyTextToClipboard(screen, nil, clipboardCopyText))

	assert.Equal(t, clipboardCopyText, string(screen.clipboard))
}

func TestCopyTextToClipboardIgnoresEmptyText(t *testing.T) {
	t.Parallel()

	screen := newClipboardScreen()
	system := newFakeSystemClipboard()

	require.NoError(t, copyTextToClipboard(screen, system, ""))

	assert.Empty(t, screen.clipboard)
	assertNoClipboardWrite(t, system)
}

func TestCopyTextToClipboardHandlesMissingScreen(t *testing.T) {
	t.Parallel()

	system := newFakeSystemClipboard()

	require.NoError(t, copyTextToClipboard(nil, system, clipboardCopyText))

	assertNoClipboardWrite(t, system)
}
