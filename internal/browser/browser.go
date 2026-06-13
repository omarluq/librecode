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

const openerStartTimeout = 3 * time.Second

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
		return browserError(err, "find browser opener")
	}

	ctx, cancel := context.WithTimeout(context.Background(), openerStartTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "")
	cmd.Path = path

	cmd.Args = append([]string{path}, args...)
	if err := cmd.Start(); err != nil {
		return browserError(err, "start browser opener")
	}

	return browserError(cmd.Process.Release(), "release browser opener")
}
