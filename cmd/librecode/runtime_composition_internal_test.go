package main

import (
	"context"
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
	chatOptions := chatRunOptions{SessionID: testSessionID, ResumeID: "", Resume: false}
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
		assert.True(t, filepath.IsAbs(options.CWD))
		assert.Equal(t, chatOptions.SessionID, options.SessionID)

		tasks, err := options.Runtime.AgentTasks(ctx, "missing", 1)
		require.NoError(t, err)
		assert.Empty(t, tasks)

		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
}

func TestChatCompositionPropagatesTerminalError(t *testing.T) {
	t.Parallel()

	container := newRuntimeTestContainer(t, true)
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())

	expectedErr := assert.AnError

	err := runChatWithContainer(cmd, container, chatRunOptions{SessionID: "", ResumeID: "", Resume: false}, func(
		context.Context,
		*terminal.RunOptions,
	) error {
		return expectedErr
	})

	assert.ErrorIs(t, err, expectedErr)
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

			tasks, err := runtime.AgentTasks(gotCmd.Context(), "missing", 1)
			require.NoError(t, err)
			assert.Empty(t, tasks)

			return nil
		},
	)

	require.NoError(t, err)
	assert.True(t, called)
}

func TestPromptCompositionPropagatesRunnerError(t *testing.T) {
	t.Parallel()

	container := newRuntimeTestContainer(t, false)
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())

	expectedErr := assert.AnError

	err := runPromptWithContainerAndRunner(
		cmd,
		container,
		promptRunOptions{SessionID: "", SessionName: "", ToolStrategy: "", MetricsJSON: "", Resume: false},
		"hello",
		func(*assistant.Runtime, *cobra.Command, promptRunOptions, string) error {
			return expectedErr
		},
	)

	assert.ErrorIs(t, err, expectedErr)
}

func TestRuntimeCompositionRejectsClosedContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		run  func(*cobra.Command, *di.Container, *bool) error
		name string
	}{
		{
			name: "chat",
			run: func(cmd *cobra.Command, container *di.Container, called *bool) error {
				return runChatWithContainer(
					cmd,
					container,
					chatRunOptions{SessionID: "", ResumeID: "", Resume: false},
					func(context.Context, *terminal.RunOptions) error {
						*called = true

						return nil
					},
				)
			},
		},
		{
			name: promptUse,
			run: func(cmd *cobra.Command, container *di.Container, called *bool) error {
				return runPromptWithContainerAndRunner(
					cmd,
					container,
					promptRunOptions{
						SessionID: "", SessionName: "", ToolStrategy: "", MetricsJSON: "", Resume: false,
					},
					"hello",
					func(*assistant.Runtime, *cobra.Command, promptRunOptions, string) error {
						*called = true

						return nil
					},
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			container := newRuntimeTestContainer(t, false)
			require.True(t, container.ShutdownWithContext(context.Background()).Succeed)

			cmd := &cobra.Command{}
			cmd.SetContext(t.Context())

			called := false
			err := test.run(cmd, container, &called)

			require.ErrorContains(t, err, "start runtime services")
			assert.False(t, called)
		})
	}
}

func newRuntimeTestContainer(t *testing.T, interactive bool) *di.Container {
	t.Helper()

	container, err := di.NewContainer(
		t.Context(),
		writeLifecycleTestConfig(t),
		di.ConfigOverrides{DisableExtensions: true, Interactive: interactive},
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		report := container.ShutdownWithContext(context.Background())
		assert.True(t, report.Succeed, report.Error())
	})

	return container
}
