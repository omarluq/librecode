package main

import (
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/terminal"
)

func newChatCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Open the interactive chat UI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				runtime := di.MustInvoke[*di.AssistantService](container).Runtime
				modelRegistry := di.MustInvoke[*di.ModelService](container).Registry
				cfg := di.MustInvoke[*di.ConfigService](container).Get()
				cwd, err := assistant.DefaultCWD("")
				if err != nil {
					return err
				}

				return terminal.Run(cmd.Context(), terminal.RunOptions{
					Runtime:   runtime,
					Models:    modelRegistry,
					Config:    cfg,
					CWD:       cwd,
					SessionID: sessionID,
				})
			})
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id to append to")

	return cmd
}
