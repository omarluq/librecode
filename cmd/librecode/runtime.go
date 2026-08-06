package main

import (
	"context"
	"errors"
	"time"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/di"
)

const shutdownTimeout = 10 * time.Second

func withContainer(
	ctx context.Context,
	options commandOptions,
	handler func(*di.Container) error,
) (runErr error) {
	container, err := di.NewContainer(ctx, options.configFile, options.configOverrides())
	if err != nil {
		return cliError(err, "initialize services")
	}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		runErr = finishContainerRun(runErr, container.ShutdownWithContext(shutdownCtx))
	}()

	return handler(container)
}

func finishContainerRun(runErr error, shutdownReport *do.ShutdownReport) error {
	if shutdownReport != nil && !shutdownReport.Succeed && shutdownReport.Error() != "" {
		if runErr != nil {
			return errors.Join(runErr, shutdownReport)
		}

		return shutdownReport
	}

	return runErr
}
