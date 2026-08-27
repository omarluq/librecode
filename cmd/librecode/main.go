//go:build !windows

// Package main defines the librecode CLI entrypoint and top-level commands.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/omarluq/librecode/internal/startupprofile"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "__execute-worker" {
		if err := executeworker.Serve(os.Stdin, os.Stdout); err != nil {
			if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
				os.Exit(1)
			}

			os.Exit(1)
		}

		os.Exit(0)
	}

	os.Exit(run())
}

func run() (exitCode int) {
	profiler, err := startupprofile.Start()
	if err != nil {
		if _, writeErr := fmt.Fprintln(os.Stderr, "start startup profiler:", err); writeErr != nil {
			return 1
		}

		return 1
	}
	defer func() {
		if err := profiler.Stop(); err != nil {
			exitCode = 1

			if _, writeErr := fmt.Fprintln(os.Stderr, "stop startup profiler:", err); writeErr != nil {
				exitCode = 1
			}
		}
	}()

	profiler.Mark("main")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ctx = startupprofile.Context(ctx, profiler)

	cmd := newRootCmd()
	cmd.SetContext(ctx)
	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	if err := cmd.ExecuteContext(ctx); err != nil {
		if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
			return 1
		}

		return 1
	}

	return 0
}
