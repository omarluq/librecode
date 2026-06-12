package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/do/v2"

	"github.com/omarluq/librecode/internal/di"
)

func withContainer(ctx context.Context, handler func(*di.Container) error) error {
	container, err := di.NewContainer(cfgFile, di.ConfigOverrides{DisableExtensions: disableExtensions})
	if err != nil {
		return err
	}

	runErr := handler(container)

	return finishContainerRun(runErr, container.ShutdownWithContext(ctx))
}

func finishContainerRun(runErr error, shutdownReport *do.ShutdownReport) error {
	if shutdownReport != nil && !shutdownReport.Succeed && shutdownReport.Error() != "" {
		shutdownErr := fmt.Errorf("%w", shutdownReport)
		if runErr != nil {
			return errors.Join(runErr, shutdownErr)
		}

		return shutdownErr
	}

	return runErr
}
