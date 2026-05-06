// Package main defines the librecode CLI entrypoint and top-level commands.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"charm.land/fang/v2"

	"github.com/omarluq/librecode/internal/vinfo"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := fang.Execute(ctx, newRootCmd(), fang.WithVersion(vinfo.String())); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		return 1
	}

	return 0
}
