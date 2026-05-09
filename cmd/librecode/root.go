package main

import (
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/vinfo"
)

var cfgFile string
var disableExtensions bool

func newRootCmd() *cobra.Command {
	var resumeSession string

	cmd := &cobra.Command{
		Use:           "librecode",
		Short:         "librecode is an AI assistant for coding work",
		Version:       vinfo.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if resumeSession != "" && len(args) > 0 {
				resumeSession = args[0]
			}
			return runChat(cmd, chatRunOptions{
				SessionID: "",
				ResumeID:  resumeSession,
				Resume:    resumeSession != "",
			})
		},
	}

	cmd.Flags().StringVarP(
		&resumeSession,
		"resume",
		"r",
		"",
		"resume a session by id (defaults to latest when omitted)",
	)
	cmd.Flags().Lookup("resume").NoOptDefVal = latestSessionFlagValue
	cmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file path")
	cmd.PersistentFlags().BoolVar(&disableExtensions, "no-extensions", false, "disable Lua extensions for this run")
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
