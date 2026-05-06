package main

import (
	"fmt"
	"strings"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/agent"
	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/session"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage SQLite-backed sessions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newSessionNewCmd())
	cmd.AddCommand(newSessionListCmd())
	cmd.AddCommand(newSessionShowCmd())

	return cmd
}

func newSessionNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new [name]",
		Short: "Create a new session",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(strings.Join(args, " "))

			return withContainer(cmd.Context(), func(container *di.Container) error {
				store := di.MustInvoke[*di.DatabaseService](container).Store
				cwd, err := agent.DefaultCWD("")
				if err != nil {
					return err
				}

				createdSession, err := store.CreateSession(cmd.Context(), cwd, name, "")
				if err != nil {
					return err
				}

				return printLine(cmd.OutOrStdout(), createdSession.ID)
			})
		},
	}
}

func newSessionListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List sessions for the current working directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				store := di.MustInvoke[*di.DatabaseService](container).Store
				cwd, err := agent.DefaultCWD("")
				if err != nil {
					return err
				}

				sessions, err := store.ListSessions(cmd.Context(), cwd)
				if err != nil {
					return err
				}

				for _, listedSession := range sessions {
					if err := printSessionSummary(cmd, listedSession); err != nil {
						return err
					}
				}

				return nil
			})
		},
	}
}

func newSessionShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show entries for a session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withContainer(cmd.Context(), func(container *di.Container) error {
				store := di.MustInvoke[*di.DatabaseService](container).Store
				entries, err := store.Entries(cmd.Context(), args[0])
				if err != nil {
					return err
				}

				for _, entry := range entries {
					if err := printSessionEntry(cmd, entry); err != nil {
						return err
					}
				}

				return nil
			})
		},
	}
}

func printSessionSummary(cmd *cobra.Command, listedSession session.Session) error {
	name := listedSession.Name
	if name == "" {
		name = "(unnamed)"
	}

	_, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"%s\t%s\t%s\n",
		listedSession.ID,
		listedSession.UpdatedAt.Format("2006-01-02 15:04:05"),
		name,
	)
	if err != nil {
		return oops.Wrapf(err, "write session summary")
	}

	return nil
}

func printSessionEntry(cmd *cobra.Command, entry session.Entry) error {
	line := fmt.Sprintf("%s\t%s\t%s", entry.ID, entry.Type, entry.Message.Content)
	if entry.Summary != "" {
		line = fmt.Sprintf("%s\t%s\t%s", entry.ID, entry.Type, entry.Summary)
	}

	return printLine(cmd.OutOrStdout(), "%s", line)
}
