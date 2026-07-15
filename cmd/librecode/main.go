// Package main defines the librecode CLI entrypoint and top-level commands.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/omarluq/librecode/internal/executeworker"
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

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
