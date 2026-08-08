//go:build windows

package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/samber/lo"
	"github.com/samber/oops"
	"golang.org/x/sys/windows"
)

const (
	shellLoginArg         = "-lc"
	windowsBashExecutable = "bash.exe"
)

var errBashNotFound = errors.New("bash shell not found")

func shellConfig(command string) (shellPath string, shellArgs []string, err error) {
	bashPath, err := findWindowsBash()
	if err != nil {
		return "", nil, err
	}

	return bashPath, []string{shellLoginArg, command}, nil
}

func findWindowsBash() (string, error) {
	configured := os.Getenv("LIBRECODE_BASH_PATH")
	if configured != "" && !filepath.IsAbs(configured) {
		return "", oops.In("tool").Code("bash-discovery").With("configured_path", configured).
			Wrapf(errBashNotFound, "LIBRECODE_BASH_PATH must be an absolute path")
	}

	for _, candidate := range windowsBashCandidates() {
		if path, ok := existingExecutable(candidate); ok {
			return path, nil
		}
	}

	return "", oops.In("tool").Code("bash-discovery").Wrapf(
		errBashNotFound,
		"install Git Bash in a standard location or set LIBRECODE_BASH_PATH to an absolute path",
	)
}

func existingExecutable(candidate string) (string, bool) {
	if candidate == "" {
		return "", false
	}

	path, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}

	return path, true
}

func windowsBashCandidates() []string {
	candidates := []string{}
	if configured := os.Getenv("LIBRECODE_BASH_PATH"); filepath.IsAbs(configured) {
		candidates = append(candidates, configured)
	}

	for _, candidate := range windowsBashDirectoryCandidates() {
		candidates = append(candidates,
			filepath.Join(candidate, "Git", "bin", windowsBashExecutable),
			filepath.Join(candidate, "Git", "usr", "bin", windowsBashExecutable),
		)
	}

	return candidates
}

func windowsBashDirectoryCandidates() []string {
	candidates := lo.Compact([]string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
	})
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		candidates = append(candidates, filepath.Join(localAppData, "Programs"))
	}

	return candidates
}

func configureShellCommand(_ *exec.Cmd) {
	// Windows does not require the Unix process-group configuration.
}

func terminateShellCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	// taskkill /T is the native, broadly available process-tree terminator.
	// Bound it so cancellation cannot hang before Process.Kill gets a chance.
	ctx, cancel := context.WithTimeout(context.Background(), commandWaitDelay)
	defer cancel()

	systemDirectory, err := windows.GetSystemDirectory()
	if err == nil {
		taskkillPath := filepath.Join(systemDirectory, "taskkill.exe")
		killTree := exec.CommandContext(ctx, taskkillPath, "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F")
		if err := killTree.Run(); err == nil {
			return nil
		}
	}

	return cmd.Process.Kill()
}

func shellCommandContext(ctx context.Context, shellPath string, shellArgs []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, shellPath)
	cmd.Args = append(cmd.Args, shellArgs...)

	return cmd
}
