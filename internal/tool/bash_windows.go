//go:build windows

package tool

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const shellLoginArg = "-lc"

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
		if candidate == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf(
		"%w: install Git Bash, MSYS2/Cygwin/WSL bash, or set LIBRECODE_BASH_PATH to bash.exe",
		errBashNotFound,
	)
}

func windowsBashCandidates() []string {
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	localAppData := os.Getenv("LOCALAPPDATA")

	return []string{
		os.Getenv("LIBRECODE_BASH_PATH"),
		filepath.Join(programFiles, "Git", "bin", "bash.exe"),
		filepath.Join(programFiles, "Git", "usr", "bin", "bash.exe"),
		filepath.Join(programFilesX86, "Git", "bin", "bash.exe"),
		filepath.Join(programFilesX86, "Git", "usr", "bin", "bash.exe"),
		filepath.Join(localAppData, "Programs", "Git", "bin", "bash.exe"),
		filepath.Join(localAppData, "Programs", "Git", "usr", "bin", "bash.exe"),
		"bash.exe",
		"bash",
	}
}

func configureShellCommand(_ *exec.Cmd) {}

func terminateShellCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	return cmd.Process.Kill()
}
