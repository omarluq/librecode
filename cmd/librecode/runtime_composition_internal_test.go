package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/terminal"
)

func TestChatCompositionStartsRuntimeAndInvokesTerminal(t *testing.T) {
	t.Parallel()

	container := newRuntimeTestContainer(t, true)
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())

	called := false
	chatOptions := chatRunOptions{SessionID: "", ResumeID: "", Resume: false}
	err := runChatWithContainer(cmd, container, chatOptions, func(
		ctx context.Context,
		options *terminal.RunOptions,
	) error {
		called = true

		assert.Same(t, cmd.Context(), ctx)
		require.NotNil(t, options)
		require.NotNil(t, options.Runtime)
		require.NotNil(t, options.Workflows)
		require.NotNil(t, options.Settings)
		require.NotNil(t, options.Models)
		require.NotNil(t, options.Auth)
		assert.NotNil(t, options.Config)

		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestPromptCompositionStartsRuntimeAndInvokesRunner(t *testing.T) {
	t.Parallel()

	container := newRuntimeTestContainer(t, false)
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())

	called := false
	promptOptions := promptRunOptions{
		SessionID: "", SessionName: "", ToolStrategy: "", MetricsJSON: "", Resume: false,
	}
	err := runPromptWithContainerAndRunner(
		cmd,
		container,
		promptOptions,
		"hello",
		func(runtime *assistant.Runtime, gotCmd *cobra.Command, options promptRunOptions, message string) error {
			called = true

			require.NotNil(t, runtime)
			assert.Same(t, cmd, gotCmd)
			assert.Equal(t, "hello", message)
			assert.Equal(t, promptOptions, options)

			return nil
		},
	)

	require.NoError(t, err)
	assert.True(t, called)
}

func newRuntimeTestContainer(t *testing.T, interactive bool) *di.Container {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "librecode.db")
	config := fmt.Sprintf(
		"database:\n  path: %q\nmodels:\n  discovery:\n    enabled: false\nextensions:\n  use: []\n",
		databasePath,
	)
	container, err := di.NewContainer(
		t.Context(),
		writeTestConfig(t, config),
		di.ConfigOverrides{DisableExtensions: true, Interactive: interactive},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.True(t, report.Succeed, report.Error())
	})

	return container
}
