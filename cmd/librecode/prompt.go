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

	cmd := &cobra.Command{
		Use:   "prompt [message]",
		Short: "Send a prompt through the local assistant runtime",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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

				response, err := runtime.Prompt(cmd.Context(), assistant.PromptRequest{
					ParentEntryID: nil,
					SessionID:     sessionID,
					CWD:           cwd,
					Text:          message,
					Name:          sessionName,
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
