package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/omarluq/librecode/internal/di"
)

func withContainer(ctx context.Context, handler func(*di.Container) error) error {
	container, err := di.NewContainer(cfgFile)
	if err != nil {
		return err
	}

	runErr := handler(container)
	shutdownReport := container.ShutdownWithContext(ctx)
	if !shutdownReport.Succeed && shutdownReport.Error() != "" {
		shutdownErr := fmt.Errorf("%w", shutdownReport)
		if runErr != nil {
			return errors.Join(runErr, shutdownErr)
		}

		return shutdownErr
	}

	return runErr
}
