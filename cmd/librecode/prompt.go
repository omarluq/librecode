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

func newPromptCmd() *cobra.Command {
	var sessionID string
	var sessionName string
	var resume bool

	cmd := &cobra.Command{
		Use:   "prompt [message]",
		Short: "Send a prompt through the assistant runtime",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if resume && sessionID != "" {
				return fmt.Errorf("--resume cannot be used with --session")
			}
			if resume && sessionName != "" {
				return fmt.Errorf("--resume cannot be used with --name")
			}
			message, err := promptMessage(cmd, args)
			if err != nil {
				return err
			}

			return withContainer(cmd.Context(), func(container *di.Container) error {
				runtime := di.MustInvoke[*di.AssistantService](container).Runtime
				cwd, err := assistant.DefaultCWD("")
				if err != nil {
					return err
				}

				response, err := runtime.Prompt(cmd.Context(), &assistant.PromptRequest{
					OnEvent:       nil,
					OnUserEntry:   nil,
					ParentEntryID: nil,
					SessionID:     sessionID,
					CWD:           cwd,
					Text:          message,
					Name:          sessionName,
					ResumeLatest:  resume,
				})
				if err != nil {
					return err
				}

				if _, err := fmt.Fprintln(cmd.OutOrStdout(), response.Text); err != nil {
					return oops.Wrapf(err, "write prompt response")
				}

				return nil
			})
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id to append to")
	cmd.Flags().StringVar(&sessionName, "name", "", "create a named session")
	cmd.Flags().BoolVar(&resume, "resume", false, "resume the latest session for this working directory")

	return cmd
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
