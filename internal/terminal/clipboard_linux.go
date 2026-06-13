//go:build linux

package terminal

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/omarluq/librecode/internal/execpath"
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
	ctx, cancel := context.WithTimeout(context.Background(), clipboardTimeout)
	defer cancel()

	cmd, err := execpath.CommandContext(ctx, name, args...)
	if err != nil {
		return terminalError(err, "find clipboard command")
	}

	cmd.Stdin = strings.NewReader(text)

	return terminalError(cmd.Run(), "run clipboard command")
}
