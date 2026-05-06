package main

import (
	"fmt"
	"strings"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/plugin"
)

func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage Lua plugins",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newPluginListCmd())
	cmd.AddCommand(newPluginRunCmd())

	return cmd
}

func newPluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List loaded Lua plugins, commands, and tools",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				manager := di.MustInvoke[*di.PluginService](container).Manager
				for _, loadedPlugin := range manager.Plugins() {
					if err := printPlugin(cmd, loadedPlugin); err != nil {
						return err
					}
				}

				return nil
			})
		},
	}
}

func newPluginRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <command> [args...]",
		Short: "Run a Lua plugin command",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				manager := di.MustInvoke[*di.PluginService](container).Manager
				result, err := manager.ExecuteCommand(cmd.Context(), args[0], strings.Join(args[1:], " "))
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), "%s", result)
			})
		},
	}
}

func printPlugin(cmd *cobra.Command, loadedPlugin plugin.LoadedPlugin) error {
	_, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"%s\t%s\tcommands=%s\ttools=%s\n",
		loadedPlugin.Name,
		loadedPlugin.Path,
		strings.Join(loadedPlugin.Commands, ","),
		strings.Join(loadedPlugin.Tools, ","),
	)
	if err != nil {
		return oops.Wrapf(err, "write plugin")
	}

	return nil
}
