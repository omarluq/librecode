package main

import (
	"fmt"
	"strings"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/extension"
)

func newExtensionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extension",
		Short: "Manage workflow extensions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newExtensionListCmd())
	cmd.AddCommand(newExtensionRunCmd())

	return cmd
}

func newExtensionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   listUse,
		Short: "List loaded workflow extensions, commands, and tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				manager := di.MustInvoke[*di.ExtensionService](container).Manager
				extensions := manager.Extensions()
				for index := range extensions {
					if err := printExtension(cmd, &extensions[index]); err != nil {
						return err
					}
				}

				return nil
			})
		},
	}
}

func newExtensionRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <command> [args...]",
		Short: "Run a workflow extension command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				manager := di.MustInvoke[*di.ExtensionService](container).Manager
				result, err := manager.ExecuteCommand(cmd.Context(), args[0], strings.Join(args[1:], " "))
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "%s", result)
			})
		},
	}
}

func printExtension(cmd *cobra.Command, loadedExtension *extension.LoadedExtension) error {
	_, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"%s\t%s\tcommands=%s\ttools=%s\tcomposer=%s\n",
		loadedExtension.Name,
		loadedExtension.Path,
		strings.Join(loadedExtension.Commands, ","),
		strings.Join(loadedExtension.Tools, ","),
		strings.Join(loadedExtension.ComposerModes, ","),
	)
	if err != nil {
		return oops.Wrapf(err, "write extension")
	}

	return nil
}
