package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/config"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage application configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newConfigShowCmd())
	cmd.AddCommand(newConfigValidateCmd())

	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display resolved configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			entries := configEntries(cfg)
			env := resolveEnv(cfg.App.Env, "development")
			envKeys := upperEnvKeys("LIBRECODE", entries)

			keys := lo.Map(entries, func(e configEntry, _ int) string { return e.key })
			sort.Strings(keys)

			lookup := lo.SliceToMap(entries, func(e configEntry) (string, string) {
				return e.key, e.value
			})

			maxLen := lo.MaxBy(keys, func(a, b string) bool { return len(a) > len(b) })
			writer := cmd.OutOrStdout()

			if err := printLine(writer, "Environment: %s", env); err != nil {
				return err
			}

			if err := printLine(writer, "Env vars:    %s", strings.Join(envKeys, ", ")); err != nil {
				return err
			}

			if err := printLine(writer, ""); err != nil {
				return err
			}

			for _, key := range keys {
				if err := printLine(writer, "%-*s  %s", len(maxLen), key, lookup[key]); err != nil {
					return err
				}
			}

			return nil
		},
	}
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration and report errors",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := loadConfig(); err != nil {
				return err
			}

			return printLine(cmd.OutOrStdout(), "configuration is valid")
		},
	}
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.Load(cfgFile).Get()
	if err != nil {
		return nil, oops.
			In("config").
			Code("invalid_config").
			Wrapf(err, "load configuration")
	}

	return cfg, nil
}

func printLine(w io.Writer, format string, args ...any) error {
	if _, err := fmt.Fprintf(w, format+"\n", args...); err != nil {
		return oops.Wrapf(err, "write output")
	}

	return nil
}

type configEntry struct {
	key   string
	value string
}

func configEntries(cfg *config.Config) []configEntry {
	return []configEntry{
		{key: "app.name", value: cfg.App.Name},
		{key: "app.env", value: cfg.App.Env},
		{key: "logging.level", value: cfg.Logging.Level},
		{key: "logging.format", value: cfg.Logging.Format},
	}
}

// resolveEnv returns the environment label, falling back to the provided default.
func resolveEnv(env, fallback string) string {
	return mo.EmptyableToOption(env).OrElse(fallback)
}

// upperEnvKeys returns config keys uppercased with a given prefix (e.g. "LIBRECODE_APP_NAME").
func upperEnvKeys(prefix string, entries []configEntry) []string {
	return lo.Map(entries, func(e configEntry, _ int) string {
		return strings.ToUpper(prefix + "_" + strings.ReplaceAll(e.key, ".", "_"))
	})
}
