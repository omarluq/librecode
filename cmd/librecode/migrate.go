package main

import (
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/di"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply database migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := printLine(cmd.OutOrStdout(), "applying database migrations..."); err != nil {
				return err
			}

			return withContainer(cmd.Context(), commandOptionsFromCommand(cmd), func(container *di.Container) error {
				databaseService, err := container.DatabaseService()
				if err != nil {
					return cliError(err, cliResolveDatabaseService)
				}

				return printLine(cmd.OutOrStdout(), "migrations applied: %s", databaseService.Path())
			})
		},
	}
}
