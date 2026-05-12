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
		Short: "List configured and loaded workflow extensions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				service := di.MustInvoke[*di.ExtensionService](container)
				loadedByPath := loadedExtensionsByPath(service.Manager.Extensions())
				for index := range service.State.Configured {
					configuredExtension := &service.State.Configured[index]
					if err := printConfiguredExtension(cmd, configuredExtension, loadedByPath); err != nil {
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

func printConfiguredExtension(
	cmd *cobra.Command,
	configuredExtension *extension.ResolvedSource,
	loadedByPath map[string]extension.LoadedExtension,
) error {
	loadedExtension, loaded := loadedByPath[configuredExtension.LoadPath]
	status := configuredExtension.Status
	if loaded {
		status = "loaded"
	}

	_, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"%s\t%s\t%s\tversion=%s\tpath=%s\tcommands=%s\ttools=%s\tkeymaps=%s\thandlers=%s\ttimers=%d\tduration=%s\n",
		configuredExtension.Name,
		configuredExtension.Ref.Key(),
		status,
		configuredExtension.Lock.Version,
		configuredExtension.LoadPath,
		strings.Join(loadedExtension.Commands, ","),
		strings.Join(loadedExtension.Tools, ","),
		strings.Join(loadedExtension.Keymaps, ","),
		strings.Join(loadedExtension.Handlers, ","),
		loadedExtension.Timers,
		loadedExtension.TotalDuration,
	)
	if err != nil {
		return oops.Wrapf(err, "write extension")
	}

	return nil
}

func loadedExtensionsByPath(loadedExtensions []extension.LoadedExtension) map[string]extension.LoadedExtension {
	loadedByPath := make(map[string]extension.LoadedExtension, len(loadedExtensions))
	for index := range loadedExtensions {
		loadedExtension := loadedExtensions[index]
		loadedByPath[loadedExtension.Path] = loadedExtension
	}

	return loadedByPath
}
