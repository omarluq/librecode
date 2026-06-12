package main

import (
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/vinfo"
)

type commandOptions struct {
	configFile        string
	disableExtensions bool
}

func commandOptionsFromCommand(cmd *cobra.Command) commandOptions {
	root := cmd.Root()
	configFile, err := root.PersistentFlags().GetString("config")
	if err != nil {
		configFile = ""
	}
	disableExtensions, err := root.PersistentFlags().GetBool("no-extensions")
	if err != nil {
		disableExtensions = false
	}

	return commandOptions{configFile: configFile, disableExtensions: disableExtensions}
}

func (options commandOptions) configOverrides() di.ConfigOverrides {
	return di.ConfigOverrides{DisableExtensions: options.disableExtensions}
}

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
	cmd.PersistentFlags().String("config", "", "config file path")
	cmd.PersistentFlags().Bool("no-extensions", false, "disable Lua extensions for this run")
	cmd.AddCommand(newChatCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newMigrateCmd())
	cmd.AddCommand(newModelCmd())
	cmd.AddCommand(newExtensionCmd())
	cmd.AddCommand(newPromptCmd())
	cmd.AddCommand(newSessionCmd())
	cmd.AddCommand(newSkillCmd())
	cmd.AddCommand(newToolCmd())
	cmd.AddCommand(newVersionCmd())

	return cmd
}
