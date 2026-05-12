// Package browser opens URLs with the user's system browser.
package browser

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// ErrNoOpener indicates no browser opener command is available.
var ErrNoOpener = errors.New("no browser opener available")

// Open asks the operating system to open targetURL in the user's browser.
func Open(targetURL string) error {
	if targetURL == "" {
		return nil
	}
	for _, command := range openerCommands() {
		if err := runOpener(command.name, append(command.args, targetURL)...); err == nil {
			return nil
		}
	}

	return ErrNoOpener
}

type openerCommand struct {
	name string
	args []string
}

func openerCommands() []openerCommand {
	if configured := os.Getenv("BROWSER"); configured != "" {
		return append([]openerCommand{{name: configured, args: []string{}}}, platformOpeners()...)
	}

	return platformOpeners()
}

func platformOpeners() []openerCommand {
	switch runtime.GOOS {
	case "darwin":
		return []openerCommand{{name: "open", args: []string{}}}
	case "windows":
		return []openerCommand{{name: "rundll32", args: []string{"url.dll,FileProtocolHandler"}}}
	default:
		return []openerCommand{
			{name: "xdg-open", args: []string{}},
			{name: "gio", args: []string{"open"}},
		}
	}
}

func runOpener(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	//nolint:gosec // Browser opener command comes from BROWSER or a fixed platform allowlist.
	cmd := exec.CommandContext(ctx, path, args...)
	if err := cmd.Start(); err != nil {
		return err
	}

	return cmd.Process.Release()
}
