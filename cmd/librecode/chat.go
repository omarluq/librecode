package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/terminal"
)

const latestSessionFlagValue = "__latest__"

type chatRunOptions struct {
	SessionID string
	ResumeID  string
	Resume    bool
}

func newChatCmd() *cobra.Command {
	var options chatRunOptions

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Open the interactive chat UI",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.ResumeID != "" && len(args) > 0 {
				options.ResumeID = args[0]
			}
			options.Resume = options.ResumeID != ""
			return runChat(cmd, options)
		},
	}

	cmd.Flags().StringVar(&options.SessionID, "session", "", "session id to append to")
	cmd.Flags().StringVarP(
		&options.ResumeID,
		"resume",
		"r",
		"",
		"resume a session by id (defaults to latest when omitted)",
	)
	cmd.Flags().Lookup("resume").NoOptDefVal = latestSessionFlagValue

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

		return terminal.Run(cmd.Context(), &terminal.RunOptions{
			Extensions: extensionManager,
			Resources:  &resources,
			Runtime:    runtime,
			Settings:   databaseService.Documents,
			Models:     modelRegistry,
			Auth:       authStorage,
			Config:     cfg,
			CWD:        cwd,
			SessionID:  sessionID,
		})
	})
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
	if options.ResumeID != "" && options.ResumeID != latestSessionFlagValue {
		return options.ResumeID, nil
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
