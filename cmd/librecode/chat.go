package main

import (
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/tui"
)

func newChatCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Open the tcell interactive chat UI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				runtime := di.MustInvoke[*di.AgentService](container).Runtime
				cwd, err := agent.DefaultCWD("")
				if err != nil {
					return err
				}

				return tui.Run(cmd.Context(), runtime, cwd, sessionID)
			})
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "session id to append to")

	return cmd
}
