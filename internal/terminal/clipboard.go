package terminal

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.design/x/clipboard"
)

var errSystemClipboardWriteFailed = errors.New("system clipboard write failed")

type systemClipboardWriter interface {
	WriteText(text string) error
}

type desktopClipboard struct {
	prepare func()
	init    func() error
	write   func(clipboard.Format, []byte) <-chan struct{}
}

func newDesktopClipboard() desktopClipboard {
	return desktopClipboard{
		prepare: prepareDesktopClipboardEnvironment,
		init:    clipboard.Init,
		write:   clipboard.Write,
	}
}

func prepareDesktopClipboardEnvironment() {
	if os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("XDG_RUNTIME_DIR") == "" {
		return
	}

	display := candidateWaylandDisplay(os.Getenv("XDG_RUNTIME_DIR"), filepath.Glob, os.Stat)
	if display == "" {
		return
	}

	if err := os.Setenv("WAYLAND_DISPLAY", display); err != nil {
		return
	}
}

func candidateWaylandDisplay(
	runtimeDir string,
	glob func(string) ([]string, error),
	stat func(string) (fs.FileInfo, error),
) string {
	matches, err := glob(filepath.Join(runtimeDir, "wayland-*"))
	if err != nil {
		return ""
	}

	for _, path := range matches {
		if strings.HasSuffix(path, ".lock") {
			continue
		}

		info, err := stat(path)
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			continue
		}

		return filepath.Base(path)
	}

	return ""
}

func (writer desktopClipboard) WriteText(text string) error {
	if text == "" {
		return nil
	}

	if writer.prepare != nil {
		writer.prepare()
	}

	if err := writer.init(); err != nil {
		return terminalError(err, "init system clipboard")
	}

	if changed := writer.write(clipboard.FmtText, []byte(text)); changed == nil {
		return errSystemClipboardWriteFailed
	}

	return nil
}
