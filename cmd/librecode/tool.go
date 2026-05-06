package main

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/di"
	builtintool "github.com/omarluq/librecode/internal/tool"
)

func newToolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool",
		Short: "Run Pi-style built-in coding tools",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newToolListCmd())
	cmd.AddCommand(newToolRunCmd())

	return cmd
}

func newToolListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   listUse,
		Short: "List built-in coding tools",
		Args:  cobra.NoArgs,
		RunE:  runToolList,
	}
}

func runToolList(cmd *cobra.Command, _ []string) error {
	return withContainer(cmd.Context(), func(container *di.Container) error {
		registry := di.MustInvoke[*di.ToolService](container).Registry
		definitions := registry.Definitions()
		for index := range definitions {
			if err := printToolDefinition(cmd, &definitions[index]); err != nil {
				return err
			}
		}

		return nil
	})
}

func newToolRunCmd() *cobra.Command {
	var cwd string

	cmd := &cobra.Command{
		Use:   "run <name> [json-args|-]",
		Short: "Run a built-in coding tool with JSON arguments",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := toolPayload(cmd, args[1:])
			if err != nil {
				return err
			}

			return withContainer(cmd.Context(), func(container *di.Container) error {
				service := di.MustInvoke[*di.ToolService](container)
				registry, err := toolRegistryForCWD(service, cwd)
				if err != nil {
					return err
				}
				result, err := registry.ExecuteJSON(cmd.Context(), args[0], payload)
				if err != nil {
					return err
				}

				text := strings.TrimRight(result.Text(), "\n")
				if text == "" {
					return nil
				}

				return printLine(cmd.OutOrStdout(), "%s", text)
			})
		},
	}

	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory for path-based tools")

	return cmd
}

func printToolDefinition(cmd *cobra.Command, definition *builtintool.Definition) error {
	return printLine(
		cmd.OutOrStdout(),
		"%s\tread_only=%t\t%s",
		definition.Name,
		definition.ReadOnly,
		definition.Description,
	)
}

func toolRegistryForCWD(service *di.ToolService, cwd string) (*builtintool.Registry, error) {
	if strings.TrimSpace(cwd) == "" {
		return service.Registry, nil
	}
	absoluteCWD, err := filepath.Abs(cwd)
	if err != nil {
		return nil, oops.In("tool").Code("resolve_cwd").Wrapf(err, "resolve tool cwd")
	}

	return builtintool.NewRegistry(absoluteCWD), nil
}

func toolPayload(cmd *cobra.Command, args []string) ([]byte, error) {
	if len(args) == 0 {
		return []byte("{}"), nil
	}
	if len(args) == 1 && args[0] == "-" {
		payload, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return []byte{}, oops.Wrapf(err, "read tool args")
		}
		if strings.TrimSpace(string(payload)) == "" {
			return []byte("{}"), nil
		}

		return payload, nil
	}

	return []byte(strings.Join(args, " ")), nil
}
