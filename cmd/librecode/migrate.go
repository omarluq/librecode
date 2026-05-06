package main

import (
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/di"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply SQLite session migrations",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				database := di.MustInvoke[*di.DatabaseService](container)
				return printLine(cmd.OutOrStdout(), "migrations applied: %s", database.Path())
			})
		},
	}
}
