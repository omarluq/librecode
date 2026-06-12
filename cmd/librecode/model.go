package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/model"
)

func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Inspect available models",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return newModelListCmd().RunE(cmd, nil)
		},
	}

	cmd.AddCommand(newModelListCmd())

	return cmd
}

func newModelListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list [search]",
		Short: "List models for authorized providers",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options := commandOptionsFromCommand(cmd)
			container, err := di.NewContainer(options.configFile, options.configOverrides())
			if err != nil {
				return err
			}
			defer func() {
				if report := container.ShutdownWithContext(cmd.Context()); report != nil && !report.Succeed {
					if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "shutdown failed: %v\n", report.Errors); err != nil {
						return
					}
				}
			}()

			registry := di.MustInvoke[*di.ModelService](container).Registry
			models := listedModels(registry, all)
			if len(args) == 1 {
				models = filterModelList(models, args[0])
			}

			return printModels(cmd.OutOrStdout(), models)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include models from unauthorized providers")

	return cmd
}

func listedModels(registry *model.Registry, all bool) []model.Model {
	if all {
		return registry.All()
	}

	return registry.Available()
}

func filterModelList(models []model.Model, query string) []model.Model {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return models
	}

	return lo.Filter(models, func(candidate model.Model, _ int) bool {
		haystack := strings.ToLower(candidate.Provider + " " + candidate.ID + " " + candidate.Name)
		return strings.Contains(haystack, query)
	})
}

func printModels(writer io.Writer, models []model.Model) error {
	sort.Slice(models, func(leftIndex, rightIndex int) bool {
		left := models[leftIndex].Provider + "/" + models[leftIndex].ID
		right := models[rightIndex].Provider + "/" + models[rightIndex].ID

		return left < right
	})
	rows := lo.Map(models, func(candidate model.Model, _ int) modelListRow {
		return modelListRow{
			Provider:  candidate.Provider,
			Model:     candidate.ID,
			Context:   formatTokenCount(candidate.ContextWindow),
			MaxOutput: formatTokenCount(candidate.MaxTokens),
			Reasoning: yesNo(candidate.Reasoning),
			Images:    yesNo(modelSupportsImage(&candidate)),
		}
	})
	widths := computeModelListWidths(rows)
	if _, err := fmt.Fprintf(
		writer,
		"%-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
		widths.Provider,
		"provider",
		widths.Model,
		"model",
		widths.Context,
		"context",
		widths.MaxOutput,
		"max-out",
		widths.Reasoning,
		"reasoning",
		widths.Images,
		"images",
	); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintf(
			writer,
			"%-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
			widths.Provider,
			row.Provider,
			widths.Model,
			row.Model,
			widths.Context,
			row.Context,
			widths.MaxOutput,
			row.MaxOutput,
			widths.Reasoning,
			row.Reasoning,
			widths.Images,
			row.Images,
		); err != nil {
			return err
		}
	}

	return nil
}

type modelListRow struct {
	Provider  string
	Model     string
	Context   string
	MaxOutput string
	Reasoning string
	Images    string
}

type modelListWidths struct {
	Provider  int
	Model     int
	Context   int
	MaxOutput int
	Reasoning int
	Images    int
}

func computeModelListWidths(rows []modelListRow) modelListWidths {
	widths := modelListWidths{
		Provider:  len("provider"),
		Model:     len("model"),
		Context:   len("context"),
		MaxOutput: len("max-out"),
		Reasoning: len("reasoning"),
		Images:    len("images"),
	}
	for _, row := range rows {
		widths.Provider = max(widths.Provider, len(row.Provider))
		widths.Model = max(widths.Model, len(row.Model))
		widths.Context = max(widths.Context, len(row.Context))
		widths.MaxOutput = max(widths.MaxOutput, len(row.MaxOutput))
		widths.Reasoning = max(widths.Reasoning, len(row.Reasoning))
		widths.Images = max(widths.Images, len(row.Images))
	}

	return widths
}

func formatTokenCount(count int) string {
	if count >= 1_000_000 {
		millions := float64(count) / 1_000_000
		if count%1_000_000 == 0 {
			return fmt.Sprintf("%.0fM", millions)
		}

		return fmt.Sprintf("%.1fM", millions)
	}
	if count >= 1_000 {
		thousands := float64(count) / 1_000
		if count%1_000 == 0 {
			return fmt.Sprintf("%.0fK", thousands)
		}

		return fmt.Sprintf("%.1fK", thousands)
	}

	return fmt.Sprint(count)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}

	return "no"
}

func modelSupportsImage(candidate *model.Model) bool {
	return lo.Contains(candidate.Input, model.InputImage)
}
