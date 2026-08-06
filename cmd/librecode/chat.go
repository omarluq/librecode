package main

import (
	"context"
	"errors"

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
		Use:   chatUse,
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

type chatServices struct {
	database  *di.DatabaseService
	assistant *di.AssistantService
	models    *di.ModelService
	auth      *di.AuthService
	extension *di.ExtensionService
	config    *di.ConfigService
	workflows *di.ChatWorkflowService
}

func resolveChatServices(container *di.Container) (*chatServices, error) {
	runtimeServices, err := container.StartRuntime()
	if err != nil {
		return nil, cliError(err, "start runtime services")
	}

	return &chatServices{
		database: runtimeServices.Database, assistant: runtimeServices.Assistant, models: runtimeServices.Models,
		auth: runtimeServices.Auth, extension: runtimeServices.Extensions, config: runtimeServices.Config,
		workflows: runtimeServices.ChatWorkflows,
	}, nil
}

type terminalRunner func(context.Context, *terminal.RunOptions) error

func runChat(cmd *cobra.Command, options chatRunOptions) error {
	commandOptions := commandOptionsFromCommand(cmd)
	commandOptions.interactive = true

	return withContainer(cmd.Context(), commandOptions, func(container *di.Container) error {
		return runChatWithContainer(cmd, container, options, terminal.Run)
	})
}

func runChatWithContainer(
	cmd *cobra.Command,
	container *di.Container,
	options chatRunOptions,
	runTerminal terminalRunner,
) error {
	services, err := resolveChatServices(container)
	if err != nil {
		return err
	}

	runtime := services.assistant.Runtime

	cwd, err := assistant.DefaultCWD("")
	if err != nil {
		return cliError(err, cliResolveWorkingDirectory)
	}

	sessionID, err := resolveChatSessionID(cmd.Context(), runtime, cwd, options)
	if err != nil {
		return err
	}

	resources := loadTerminalResources(cmd.Context(), cwd)

	return runTerminal(cmd.Context(), &terminal.RunOptions{
		Extensions: services.extension.Manager,
		Resources:  &resources,
		Runtime:    runtime,
		Workflows:  services.workflows.Runs(),
		Settings:   services.database.Documents,
		Models:     services.models.Registry,
		Auth:       services.auth.Storage,
		Config:     services.config.Get(),
		CWD:        cwd,
		SessionID:  sessionID,
	})
}

func resolveChatSessionID(
	ctx context.Context,
	runtime *assistant.Runtime,
	cwd string,
	options chatRunOptions,
) (string, error) {
	if options.SessionID != "" && options.Resume {
		return "", errors.New("--resume cannot be used with --session")
	}

	if !options.Resume || runtime == nil {
		return options.SessionID, nil
	}

	if options.ResumeID != "" && options.ResumeID != latestSessionFlagValue {
		return options.ResumeID, nil
	}

	latestSession, found, err := runtime.SessionRepository().LatestSession(ctx, cwd)
	if err != nil {
		return "", cliError(err, "load latest session")
	}

	if !found {
		return "", nil
	}

	return latestSession.ID, nil
}
