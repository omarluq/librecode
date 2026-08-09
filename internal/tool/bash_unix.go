package tool

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

const shellLoginArg = "-lc"

func shellConfig(command string) (shellPath string, shellArgs []string) {
	if shellPath := os.Getenv("SHELL"); shellPath != "" {
		return shellPath, []string{shellLoginArg, command}
	}

	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash", []string{shellLoginArg, command}
	}

	return "/bin/sh", []string{shellLoginArg, command}
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
		return toolWrap(err, "terminate process group")
	}

	return nil
}

func shellCommandContext(ctx context.Context, shellPath string, shellArgs []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, shellPath)
	cmd.Args = append(cmd.Args, shellArgs...)

	return cmd
}
