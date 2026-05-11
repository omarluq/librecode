//go:build linux

package terminal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

var errNoClipboardWriter = errors.New("no clipboard writer available")

func writeSystemClipboard(text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if err := writeClipboardCommand(text, "wl-copy"); err == nil {
			return nil
		}
	}
	if err := writeClipboardCommand(text, "xclip", "-selection", "clipboard"); err == nil {
		return nil
	}
	if err := writeClipboardCommand(text, "xsel", "--clipboard", "--input"); err == nil {
		return nil
	}
	if err := writeClipboardCommand(text, "wl-copy"); err == nil {
		return nil
	}
	if err := writeClipboardCommand(text, "termux-clipboard-set"); err == nil {
		return nil
	}

	return errNoClipboardWriter
}

func writeClipboardCommand(text, name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	//nolint:gosec // Clipboard writer is selected from a fixed allowlist.
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(text)

	return cmd.Run()
}
