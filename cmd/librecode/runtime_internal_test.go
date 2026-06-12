package main

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/di"
)

func TestWithContainerRunsHandler(t *testing.T) {
	t.Parallel()

	options := commandOptions{configFile: writeTestConfig(t, "extensions:\n  use: []\n"), disableExtensions: true}

	called := false
	err := withContainerOptions(context.Background(), options, func(container *di.Container) error {
		called = true
		require.NotNil(t, container)

		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestWithContainerReturnsHandlerError(t *testing.T) {
	t.Parallel()

	options := commandOptions{configFile: writeTestConfig(t, "extensions:\n  use: []\n"), disableExtensions: true}

	expectedErr := errors.New("handler failed")
	err := withContainerOptions(context.Background(), options, func(*di.Container) error {
		return expectedErr
	})

	require.ErrorIs(t, err, expectedErr)
}

func TestFinishContainerRun(t *testing.T) {
	t.Parallel()

	runErr := errors.New("run failed")

	tests := []struct {
		name           string
		report         *do.ShutdownReport
		runErr         error
		expectErrIs    error
		expectContains string
		expectErr      bool
	}{
		{
			name:           "run and shutdown errors",
			report:         failedShutdownReport(errors.New("shutdown failed")),
			runErr:         runErr,
			expectErrIs:    runErr,
			expectContains: "shutdown failed",
			expectErr:      true,
		},
		{
			name:           "shutdown error only",
			report:         failedShutdownReport(errors.New("shutdown failed")),
			runErr:         nil,
			expectErrIs:    nil,
			expectContains: "shutdown failed",
			expectErr:      true,
		},
		{
			name:           "run error only",
			report:         nil,
			runErr:         runErr,
			expectErrIs:    runErr,
			expectContains: "",
			expectErr:      true,
		},
		{
			name:           "nil shutdown report success",
			report:         nil,
			runErr:         nil,
			expectErrIs:    nil,
			expectContains: "",
			expectErr:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := finishContainerRun(test.runErr, test.report)
			if !test.expectErr {
				require.NoError(t, err)
				return
			}

			require.Error(t, err)
			if test.expectErrIs != nil {
				require.ErrorIs(t, err, test.expectErrIs)
			}
			if test.expectContains != "" {
				assert.Contains(t, err.Error(), test.expectContains)
			}
		})
	}
}

func TestWithContainerReturnsConfigError(t *testing.T) {
	t.Parallel()

	options := commandOptions{
		configFile:        writeTestConfig(t, "database:\n  busy_timeout: -1s\nextensions:\n  use: []\n"),
		disableExtensions: true,
	}

	err := withContainerOptions(context.Background(), options, func(*di.Container) error {
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.busy_timeout cannot be negative")
}

func failedShutdownReport(err error) *do.ShutdownReport {
	return &do.ShutdownReport{
		Succeed: false,
		Errors: map[do.ServiceDescription]error{
			{Service: "database"}: err,
		},
	}
}
