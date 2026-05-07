package main

import (
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/vinfo"
)

var cfgFile string

func newRootCmd() *cobra.Command {
	var resume bool

	cmd := &cobra.Command{
		Use:           "librecode",
		Short:         "librecode is an AI assistant for coding work",
		Version:       vinfo.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChat(cmd, chatRunOptions{Resume: resume})
		},
	}

	cmd.Flags().BoolVar(&resume, "resume", false, "resume the latest session for this working directory")
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
