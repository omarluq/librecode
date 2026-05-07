package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/di"
)

type promptRunOptions struct {
	SessionID   string
	SessionName string
	Resume      bool
}

func newPromptCmd() *cobra.Command {
	var options promptRunOptions

	cmd := &cobra.Command{
		Use:   "prompt [message]",
		Short: "Send a prompt through the assistant runtime",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrompt(cmd, args, options)
		},
	}

	cmd.Flags().StringVar(&options.SessionID, "session", "", "session id to append to")
	cmd.Flags().StringVar(&options.SessionName, "name", "", "create a named session")
	cmd.Flags().BoolVar(&options.Resume, "resume", false, "resume the latest session for this working directory")

	return cmd
}

func runPrompt(cmd *cobra.Command, args []string, options promptRunOptions) error {
	if err := validatePromptRunOptions(options); err != nil {
		return err
	}
	message, err := promptMessage(cmd, args)
	if err != nil {
		return err
	}

	return withContainer(cmd.Context(), func(container *di.Container) error {
		return runPromptWithContainer(cmd, container, options, message)
	})
}

func validatePromptRunOptions(options promptRunOptions) error {
	if options.Resume && options.SessionID != "" {
		return fmt.Errorf("--resume cannot be used with --session")
	}
	if options.Resume && options.SessionName != "" {
		return fmt.Errorf("--resume cannot be used with --name")
	}

	return nil
}

func runPromptWithContainer(
	cmd *cobra.Command,
	container *di.Container,
	options promptRunOptions,
	message string,
) error {
	runtime := di.MustInvoke[*di.AssistantService](container).Runtime
	cwd, err := assistant.DefaultCWD("")
	if err != nil {
		return err
	}

	response, err := runtime.Prompt(cmd.Context(), &assistant.PromptRequest{
		OnEvent:       nil,
		OnUserEntry:   nil,
		ParentEntryID: nil,
		SessionID:     options.SessionID,
		CWD:           cwd,
		Text:          message,
		Name:          options.SessionName,
		ResumeLatest:  options.Resume,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(cmd.OutOrStdout(), response.Text); err != nil {
		return oops.Wrapf(err, "write prompt response")
	}

	return nil
}

func promptMessage(cmd *cobra.Command, args []string) (string, error) {
	message := strings.TrimSpace(strings.Join(args, " "))
	if message != "" {
		return message, nil
	}

	stdin, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", oops.Wrapf(err, "read stdin")
	}
	message = strings.TrimSpace(string(stdin))
	if message == "" {
		return "", fmt.Errorf("prompt message is required")
	}

	return message, nil
}
