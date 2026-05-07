package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/terminal"
)

type chatRunOptions struct {
	SessionID string
	Resume    bool
}

func newChatCmd() *cobra.Command {
	var options chatRunOptions

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Open the interactive chat UI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChat(cmd, options)
		},
	}

	cmd.Flags().StringVar(&options.SessionID, "session", "", "session id to append to")
	cmd.Flags().BoolVar(&options.Resume, "resume", false, "resume the latest session for this working directory")

	return cmd
}

func runChat(cmd *cobra.Command, options chatRunOptions) error {
	return withContainer(cmd.Context(), func(container *di.Container) error {
		databaseService := di.MustInvoke[*di.DatabaseService](container)
		runtime := di.MustInvoke[*di.AssistantService](container).Runtime
		modelRegistry := di.MustInvoke[*di.ModelService](container).Registry
		authStorage := di.MustInvoke[*di.AuthService](container).Storage
		extensionManager := di.MustInvoke[*di.ExtensionService](container).Manager
		cfg := di.MustInvoke[*di.ConfigService](container).Get()
		cwd, err := assistant.DefaultCWD("")
		if err != nil {
			return err
		}
		sessionID, err := resolveChatSessionID(cmd.Context(), runtime, cwd, options)
		if err != nil {
			return err
		}

		resources := loadTerminalResources(cmd.Context(), cwd)

		composerMode := activeComposerMode(extensionManager.ComposerModes())

		return terminal.Run(cmd.Context(), &terminal.RunOptions{
			Resources:     &resources,
			Runtime:       runtime,
			Settings:      databaseService.Documents,
			Models:        modelRegistry,
			Auth:          authStorage,
			Config:        cfg,
			CWD:           cwd,
			SessionID:     sessionID,
			ComposerMode:  composerMode.Name,
			ComposerLabel: composerMode.Label,
			Composer:      extensionManager,
		})
	})
}

func activeComposerMode(modes []extension.ComposerMode) extension.ComposerMode {
	for _, mode := range modes {
		if mode.Default {
			return mode
		}
	}

	return extension.ComposerMode{
		Name:        "",
		Description: "",
		Extension:   "",
		Label:       "",
		Default:     false,
	}
}

func resolveChatSessionID(
	ctx context.Context,
	runtime *assistant.Runtime,
	cwd string,
	options chatRunOptions,
) (string, error) {
	if options.SessionID != "" && options.Resume {
		return "", fmt.Errorf("--resume cannot be used with --session")
	}
	if !options.Resume || runtime == nil {
		return options.SessionID, nil
	}

	latestSession, found, err := runtime.SessionRepository().LatestSession(ctx, cwd)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}

	return latestSession.ID, nil
}
