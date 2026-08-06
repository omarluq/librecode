package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/di"
)

func TestWithContainerRunsHandler(t *testing.T) {
	t.Parallel()

	options := commandOptions{
		configFile:        writeTestConfig(t, "extensions:\n  use: []\n"),
		disableExtensions: true,
		interactive:       true,
	}

	called := false
	err := withContainer(context.Background(), options, func(container *di.Container) error {
		called = true

		require.NotNil(t, container)
		configService, resolveErr := container.ConfigService()
		require.NoError(t, resolveErr)
		assert.True(t, configService.Interactive())

		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestWithContainerUsesFreshShutdownContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	options := commandOptions{
		configFile:        writeTestConfig(t, "extensions:\n  use: []\n"),
		disableExtensions: true,
		interactive:       false,
	}

	err := withContainer(ctx, options, func(container *di.Container) error {
		_, resolveErr := container.DatabaseService()
		require.NoError(t, resolveErr)
		cancel()

		return context.Canceled
	})

	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "shutdown database")
}

func TestWithContainerReturnsHandlerError(t *testing.T) {
	t.Parallel()

	options := commandOptions{
		configFile:        writeTestConfig(t, "extensions:\n  use: []\n"),
		disableExtensions: true,
		interactive:       false,
	}

	expectedErr := errors.New("handler failed")
	err := withContainer(context.Background(), options, func(*di.Container) error {
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
		interactive:       false,
	}

	err := withContainer(context.Background(), options, func(*di.Container) error {
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "database.busy_timeout cannot be negative")
}

func TestCommandsReturnBootstrapErrors(t *testing.T) {
	t.Parallel()

	configPath := writeTestConfig(t, "database:\n  busy_timeout: -1s\nextensions:\n  use: []\n")

	tests := []struct {
		name string
		args []string
	}{
		{name: chatUse, args: []string{chatUse}},
		{name: "prompt", args: []string{"prompt", "hello"}},
		{name: "model list", args: []string{modelUse, listUse}},
		{name: "tool list", args: []string{toolUse, listUse}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cmd := newRootCmd()
			cmd.SetArgs(append([]string{configFlag, configPath, noExtensionsFlag}, test.args...))

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "database.busy_timeout cannot be negative")
		})
	}
}

func TestModelAndToolCommandsUseApplicationLifecycle(t *testing.T) {
	t.Parallel()

	configPath := writeTestConfig(t, "models:\n  discovery:\n    enabled: false\nextensions:\n  use: []\n")

	t.Run("model list", func(t *testing.T) {
		t.Parallel()

		cmd := newRootCmd()
		output := new(bytes.Buffer)
		cmd.SetOut(output)
		cmd.SetArgs([]string{configFlag, configPath, noExtensionsFlag, modelUse, listUse, "--all"})

		require.NoError(t, cmd.Execute())
		assert.Contains(t, output.String(), "provider")
	})

	t.Run("read tool", func(t *testing.T) {
		t.Parallel()

		cwd := t.TempDir()
		path := filepath.Join(cwd, "hello.txt")
		writeCLIFile(t, path, "hello")

		cmd := newRootCmd()
		output := new(bytes.Buffer)
		cmd.SetOut(output)
		cmd.SetArgs([]string{
			configFlag, configPath, noExtensionsFlag, toolUse, "run", "--cwd", cwd,
			"read", `{"path":"hello.txt"}`,
		})

		require.NoError(t, cmd.Execute())
		assert.Contains(t, output.String(), "hello")
	})
}

func failedShutdownReport(err error) *do.ShutdownReport {
	return &do.ShutdownReport{
		Succeed: false,
		Errors: map[do.ServiceDescription]error{
			{Service: "database"}: err,
		},
	}
}
