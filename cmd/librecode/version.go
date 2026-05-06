package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/vinfo"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), vinfo.String())

			return err
		},
	}
}
