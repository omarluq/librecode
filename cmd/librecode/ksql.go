package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/di"
)

func newKSQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ksql",
		Short: "Inspect the configured ksqlDB backend",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newKSQLInfoCmd())
	cmd.AddCommand(newKSQLExecCmd())

	return cmd
}

func newKSQLInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Fetch ksqlDB server information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				client := di.MustInvoke[*di.KSQLService](container).Client
				if !client.Enabled() {
					return printLine(cmd.OutOrStdout(), "ksql endpoint not configured (%s)", database.KSQLProjectURL)
				}

				body, err := client.Info(cmd.Context())
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "%s", string(body))
			})
		},
	}
}

func newKSQLExecCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <statement>",
		Short: "Execute a ksqlDB statement",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				client := di.MustInvoke[*di.KSQLService](container).Client
				body, err := client.Execute(cmd.Context(), strings.Join(args, " "))
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "%s", string(body))
			})
		},
	}
}
