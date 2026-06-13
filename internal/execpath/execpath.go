// Package execpath resolves executable names without consulting the process PATH.
package execpath

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	executableBits fs.FileMode = 0o111
	windowsOS                  = "windows"
)

// Command builds a command whose executable is resolved from fixed system directories.
func Command(name string, args ...string) (*exec.Cmd, error) {
	path, err := Find(name)
	if err != nil {
		return nil, err
	}

	cmd := &exec.Cmd{
		Path: path,
		Args: append([]string{path}, args...),
	}

	return cmd, nil
}

// RunWithTimeout runs cmd and kills it if timeout elapses before it exits.
func RunWithTimeout(cmd *exec.Cmd, timeout time.Duration) error {
	if timeout <= 0 {
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("run command: %w", err)
		}

		return nil
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start command: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-done:
		return err
	case <-timer.C:
		killErr := cmd.Process.Kill()
		waitErr := <-done

		return errors.Join(
			fmt.Errorf("command timed out after %s: %w", timeout, context.DeadlineExceeded),
			killErr,
			waitErr,
		)
	}
}

// Find resolves name to an executable path from fixed system directories.
func Find(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("executable name is empty")
	}

	if filepath.IsAbs(name) {
		return validateFixedExecutable(name)
	}

	if filepath.Base(name) != name {
		return "", fmt.Errorf("executable %q must be an absolute path or bare command name", name)
	}

	for _, dir := range fixedDirs() {
		for _, candidate := range candidateNames(name) {
			path, err := validateExecutable(filepath.Join(dir, candidate))
			if err == nil {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("executable %q not found in fixed system directories", name)
}

func validateFixedExecutable(path string) (string, error) {
	path = filepath.Clean(path)
	if !isInFixedDir(path) {
		return "", fmt.Errorf("executable %q is outside fixed system directories", path)
	}

	return validateExecutable(path)
}

func validateExecutable(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("inspect executable %q: %w", path, err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("executable %q is a directory", path)
	}

	if runtime.GOOS != windowsOS && info.Mode().Perm()&executableBits == 0 {
		return "", fmt.Errorf("executable %q is not executable", path)
	}

	return path, nil
}

func isInFixedDir(path string) bool {
	for _, dir := range fixedDirs() {
		if isPathInDir(path, filepath.Clean(dir)) {
			return true
		}
	}

	return false
}

func isPathInDir(path, dir string) bool {
	relative, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}

	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func candidateNames(name string) []string {
	if runtime.GOOS != windowsOS || filepath.Ext(name) != "" {
		return []string{name}
	}

	return []string{name + ".exe", name + ".cmd", name + ".bat"}
}

func fixedDirs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/usr/bin", "/bin", "/usr/sbin", "/sbin", "/opt/homebrew/bin", "/usr/local/bin"}
	case windowsOS:
		return []string{`C:\Windows\System32`, `C:\Windows`, `C:\Windows\SysWOW64`}
	default:
		return []string{
			"/usr/bin",
			"/bin",
			"/usr/local/bin",
			"/usr/sbin",
			"/sbin",
			"/data/data/com.termux/files/usr/bin",
		}
	}
}
