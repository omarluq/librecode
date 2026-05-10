//go:build !windows

package tool

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

const shellLoginArg = "-lc"

func shellConfig(command string) (shellPath string, shellArgs []string, err error) {
	if shellPath := os.Getenv("SHELL"); shellPath != "" {
		return shellPath, []string{shellLoginArg, command}, nil
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{shellLoginArg, command}, nil
	}

	return "/bin/sh", []string{shellLoginArg, command}, nil
}

func configureShellCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateShellCommand(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	return killProcessGroup(cmd.Process.Pid)
}

func killProcessGroup(pid int) error {
	if pid <= 0 {
		return nil
	}
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}

	return nil
}
