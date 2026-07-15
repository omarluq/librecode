package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/workflow"
)

const (
	defaultWorkflowListLimit = 100
	workflowShutdownTimeout  = 10 * time.Second
)

func newWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Run, resume, inspect, and cancel workflow runs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newWorkflowRunCmd())
	cmd.AddCommand(newWorkflowSubmitCmd())
	cmd.AddCommand(newWorkflowWorkerCmd())
	cmd.AddCommand(newWorkflowResumeCmd())
	cmd.AddCommand(newWorkflowListCmd())
	cmd.AddCommand(newWorkflowGetCmd())
	cmd.AddCommand(newWorkflowEventsCmd())
	cmd.AddCommand(newWorkflowCancelCmd())

	return cmd
}

func newWorkflowRunCmd() *cobra.Command {
	var ownerSessionID string

	var name string

	var sourceVersion string

	var argumentsJSON string

	cmd := &cobra.Command{
		Use:   "run <source-file>",
		Short: "Run a durable workflow from an MVM source file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireWorkflowOwner(ownerSessionID); err != nil {
				return err
			}

			source, err := os.ReadFile(args[0])
			if err != nil {
				return cliError(err, "read workflow source")
			}

			return withWorkflowService(cmd, func(service *workflow.Service) error {
				run, _, err := service.Run(cmd.Context(), &workflow.ServiceRequest{
					Name: name, Source: string(source), SourceVersion: sourceVersion,
					ArgumentsJSON: argumentsJSON, OwnerSessionID: ownerSessionID,
					SourceLimit: 0, OutputLimit: 0,
				})
				if err != nil {
					return cliError(err, "run workflow")
				}

				return printWorkflowRun(cmd, run)
			})
		},
	}

	cmd.Flags().StringVar(&ownerSessionID, "session", "", "owner session id")
	cmd.Flags().StringVar(&name, "name", "", "workflow name")
	cmd.Flags().StringVar(&sourceVersion, "source-version", "v1", "workflow source version")
	cmd.Flags().StringVar(&argumentsJSON, "arguments", "{}", "workflow arguments JSON")

	return cmd
}

func newWorkflowSubmitCmd() *cobra.Command {
	var ownerSessionID string

	var name string

	var sourceVersion string

	var argumentsJSON string

	cmd := &cobra.Command{
		Use:   "submit <source-file>",
		Short: "Submit a workflow to durable queue workers",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireWorkflowOwner(ownerSessionID); err != nil {
				return err
			}

			source, err := os.ReadFile(args[0])
			if err != nil {
				return cliError(err, "read workflow source")
			}

			return withWorkflowService(cmd, func(service *workflow.Service) error {
				run, err := service.Submit(cmd.Context(), &workflow.ServiceRequest{
					Name: name, Source: string(source), SourceVersion: sourceVersion,
					ArgumentsJSON: argumentsJSON, OwnerSessionID: ownerSessionID,
					SourceLimit: 0, OutputLimit: 0,
				})
				if err != nil {
					return cliError(err, "submit workflow")
				}

				return printWorkflowRun(cmd, run)
			})
		},
	}

	cmd.Flags().StringVar(&ownerSessionID, "session", "", "owner session id")
	cmd.Flags().StringVar(&name, "name", "", "workflow name")
	cmd.Flags().StringVar(&sourceVersion, "source-version", "v1", "workflow source version")
	cmd.Flags().StringVar(&argumentsJSON, "arguments", "{}", "workflow arguments JSON")

	return cmd
}

func newWorkflowWorkerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Run a durable workflow queue worker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWorkflowWorker(cmd)
		},
	}
}

func runWorkflowWorker(cmd *cobra.Command) error {
	container, err := di.NewContainer(
		commandOptionsFromCommand(cmd).configFile,
		commandOptionsFromCommand(cmd).configOverrides(),
	)
	if err != nil {
		return cliError(err, "initialize services")
	}

	workflowService := container.WorkflowService()

	dispatcher, err := workflow.NewDispatcher(cmd.Context(), workflow.DispatcherOptions{
		Service:     workflowService.Runs,
		Tasks:       container.DatabaseService().Tasks,
		Logger:      nil,
		Concurrency: 0,
		Buffer:      0,
		Interval:    0,
	})
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(
			context.WithoutCancel(cmd.Context()),
			workflowShutdownTimeout,
		)
		defer cancel()

		return finishContainerRun(cliError(err, "start workflow worker"), container.ShutdownWithContext(shutdownCtx))
	}

	<-cmd.Context().Done()

	shutdownCtx, cancel := context.WithTimeout(
		context.WithoutCancel(cmd.Context()),
		workflowShutdownTimeout,
	)
	defer cancel()

	workerErr := dispatcher.Shutdown(shutdownCtx)
	report := container.ShutdownWithContext(shutdownCtx)

	return finishContainerRun(workerErr, report)
}

func newWorkflowResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <run-id>",
		Short: "Resume an interrupted workflow run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withWorkflowService(cmd, func(service *workflow.Service) error {
				run, _, err := service.Resume(cmd.Context(), args[0])
				if err != nil {
					return cliError(err, "resume workflow")
				}

				return printWorkflowRun(cmd, run)
			})
		},
	}
}

func newWorkflowListCmd() *cobra.Command {
	var ownerSessionID string

	var limit int

	cmd := &cobra.Command{
		Use:   listUse,
		Short: "List workflow runs owned by a session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireWorkflowOwner(ownerSessionID); err != nil {
				return err
			}

			return withWorkflowService(cmd, func(service *workflow.Service) error {
				runs, err := service.List(cmd.Context(), ownerSessionID, limit)
				if err != nil {
					return cliError(err, "list workflow runs")
				}

				for index := range runs {
					if err := printWorkflowRun(cmd, &runs[index]); err != nil {
						return err
					}
				}

				return nil
			})
		},
	}

	cmd.Flags().StringVar(&ownerSessionID, "session", "", "owner session id")
	cmd.Flags().IntVar(&limit, "limit", defaultWorkflowListLimit, "maximum runs to return")

	return cmd
}

func newWorkflowGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <run-id>",
		Short: "Get a workflow run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withWorkflowService(cmd, func(service *workflow.Service) error {
				run, found, err := service.Get(cmd.Context(), args[0])
				if err != nil {
					return cliError(err, "get workflow run")
				}

				if !found {
					return fmt.Errorf("workflow run %q not found", args[0])
				}

				return printWorkflowRun(cmd, run)
			})
		},
	}
}

func newWorkflowEventsCmd() *cobra.Command {
	var after int64

	var limit int

	cmd := &cobra.Command{
		Use:   "events <run-id>",
		Short: "List durable events for a workflow run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withWorkflowService(cmd, func(service *workflow.Service) error {
				events, err := service.Events(cmd.Context(), args[0], after, limit)
				if err != nil {
					return cliError(err, "list workflow events")
				}

				for index := range events {
					if err := printWorkflowEvent(cmd, &events[index]); err != nil {
						return err
					}
				}

				return nil
			})
		},
	}

	cmd.Flags().Int64Var(&after, "after", 0, "return events after this sequence")
	cmd.Flags().IntVar(&limit, "limit", defaultWorkflowListLimit, "maximum events to return")

	return cmd
}

func newWorkflowCancelCmd() *cobra.Command {
	var ownerSessionID string

	cmd := &cobra.Command{
		Use:   "cancel <run-id>",
		Short: "Cancel an active workflow run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireWorkflowOwner(ownerSessionID); err != nil {
				return err
			}

			return withWorkflowService(cmd, func(service *workflow.Service) error {
				canceled, err := service.Cancel(cmd.Context(), ownerSessionID, args[0])
				if err != nil {
					return cliError(err, "cancel workflow run")
				}

				if !canceled {
					return errors.New("workflow run not found, not owned by session, or not active")
				}

				return printLine(cmd.OutOrStdout(), "%s", args[0])
			})
		},
	}

	cmd.Flags().StringVar(&ownerSessionID, "session", "", "owner session id")

	return cmd
}

func withWorkflowService(cmd *cobra.Command, run func(*workflow.Service) error) error {
	return withContainer(cmd.Context(), commandOptionsFromCommand(cmd), func(container *di.Container) error {
		return run(container.WorkflowService().Runs)
	})
}

func requireWorkflowOwner(ownerSessionID string) error {
	if strings.TrimSpace(ownerSessionID) == "" {
		return errors.New("--session is required")
	}

	return nil
}

func printWorkflowRun(cmd *cobra.Command, run *database.WorkflowRunEntity) error {
	_, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"%s\t%s\t%s\t%s\n",
		run.Task.ID,
		run.Task.State,
		run.Task.OwnerSessionID,
		run.Task.UpdatedAt.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return oops.Wrapf(err, "write workflow run")
	}

	return nil
}

func printWorkflowEvent(cmd *cobra.Command, event *database.TaskEventEntity) error {
	return printLine(
		cmd.OutOrStdout(),
		"%s\t%s\t%s",
		strconv.FormatInt(event.Sequence, 10),
		event.Event.Kind,
		event.Event.PayloadJSON,
	)
}
