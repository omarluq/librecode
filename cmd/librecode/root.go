package main

import (
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/vinfo"
)

var cfgFile string

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "librecode",
		Short:         "librecode is a local assistant for coding work",
		Version:       vinfo.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")
	cmd.AddCommand(newChatCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newKSQLCmd())
	cmd.AddCommand(newMigrateCmd())
	cmd.AddCommand(newExtensionCmd())
	cmd.AddCommand(newPromptCmd())
	cmd.AddCommand(newSessionCmd())
	cmd.AddCommand(newToolCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}
