// Package browser opens URLs with the user's system browser.
package browser

import (
	"errors"
	"os"
	"runtime"

	"github.com/omarluq/librecode/internal/execpath"
)

// ErrNoOpener indicates no browser opener command is available.
var ErrNoOpener = errors.New("no browser opener available")

// Open asks the operating system to open targetURL in the user's browser.
func Open(targetURL string) error {
	return openWithCommands(targetURL, openerCommands())
}

func openWithCommands(targetURL string, commands []openerCommand) error {
	if targetURL == "" {
		return nil
	}

	for _, command := range commands {
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
	cmd, err := execpath.Command(name, args...)
	if err != nil {
		return browserError(err, "find browser opener")
	}

	if err := cmd.Start(); err != nil {
		return browserError(err, "start browser opener")
	}

	return browserError(cmd.Process.Release(), "release browser opener")
}
