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
	prepare func() error
	init    func() error
	write   func(clipboard.Format, []byte) <-chan struct{}
}

func newDesktopClipboard() desktopClipboard {
	return desktopClipboard{
		prepare: defaultPrepareDesktopClipboardEnvironment,
		init:    clipboard.Init,
		write:   clipboard.Write,
	}
}

func defaultPrepareDesktopClipboardEnvironment() error {
	return prepareDesktopClipboardEnvironment(os.Getenv, os.Setenv, filepath.Glob, os.Stat)
}

func prepareDesktopClipboardEnvironment(
	getenv func(string) string,
	setenv func(string, string) error,
	glob func(string) ([]string, error),
	stat func(string) (fs.FileInfo, error),
) error {
	if getenv("WAYLAND_DISPLAY") != "" || getenv("XDG_RUNTIME_DIR") == "" {
		return nil
	}

	display := candidateWaylandDisplay(getenv("XDG_RUNTIME_DIR"), glob, stat)
	if display == "" {
		return nil
	}

	if err := setenv("WAYLAND_DISPLAY", display); err != nil {
		return terminalError(err, "set wayland display")
	}

	return nil
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
		if err := writer.prepare(); err != nil {
			return terminalError(err, "prepare system clipboard")
		}
	}

	if err := writer.init(); err != nil {
		return terminalError(err, "init system clipboard")
	}

	if writer.write(clipboard.FmtText, []byte(text)) == nil {
		return errSystemClipboardWriteFailed
	}

	return nil
}
