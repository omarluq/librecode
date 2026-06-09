//go:build windows

package tool

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/samber/lo"
	"github.com/samber/oops"
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
	candidates := windowsBashCandidates()
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", oops.In("tool").Code("bash-discovery").Wrapf(
		errBashNotFound,
		"install Git Bash, MSYS2/Cygwin/WSL bash, or set LIBRECODE_BASH_PATH to %s",
		windowsBashExecutable,
	)
}

func windowsBashCandidates() []string {
	candidates := lo.Compact([]string{
		os.Getenv("LIBRECODE_BASH_PATH"),
		windowsBashExecutable,
		"bash",
	})

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
	// Windows process groups are not configured here; terminateShellCommand kills the shell process directly.
}

func terminateShellCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	return cmd.Process.Kill()
}
