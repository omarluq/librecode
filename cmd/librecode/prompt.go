package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samber/oops"
	"github.com/spf13/cobra"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/di"
	"github.com/omarluq/librecode/internal/limitio"
)

const (
	promptStdinLimitBytes int64 = 1 << 20
	promptMetricsMode           = 0o600
)

type promptRunOptions struct {
	SessionID    string
	SessionName  string
	ToolStrategy string
	MetricsJSON  string
	Resume       bool
}

func newPromptCmd() *cobra.Command {
	var options promptRunOptions

	cmd := &cobra.Command{
		Use:   "prompt [message]",
		Short: "Send a prompt through the assistant runtime",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrompt(cmd, args, options)
		},
	}

	cmd.Flags().StringVar(&options.SessionID, "session", "", "session id to append to")
	cmd.Flags().StringVar(&options.SessionName, "name", "", "create a named session")
	cmd.Flags().StringVar(&options.ToolStrategy, "tool-strategy", string(assistant.ToolStrategyHybrid),
		"tool strategy: hybrid or direct")
	cmd.Flags().StringVar(&options.MetricsJSON, "metrics-json", "", "write prompt metrics as JSON")
	cmd.Flags().BoolVar(&options.Resume, "resume", false, "resume the latest session for this working directory")

	return cmd
}

func runPrompt(cmd *cobra.Command, args []string, options promptRunOptions) error {
	if err := validatePromptRunOptions(options); err != nil {
		return err
	}

	message, err := promptMessage(cmd, args)
	if err != nil {
		return err
	}

	return withContainer(cmd.Context(), commandOptionsFromCommand(cmd), func(container *di.Container) error {
		return runPromptWithContainer(cmd, container, options, message)
	})
}

func validatePromptRunOptions(options promptRunOptions) error {
	strategy := assistant.ToolStrategy(options.ToolStrategy)
	if strategy == "" {
		strategy = assistant.ToolStrategyHybrid
	}

	if strategy != assistant.ToolStrategyHybrid && strategy != assistant.ToolStrategyDirect {
		return fmt.Errorf("invalid --tool-strategy %q: use hybrid or direct", options.ToolStrategy)
	}

	if options.Resume && options.SessionID != "" {
		return errors.New("--resume cannot be used with --session")
	}

	if options.Resume && options.SessionName != "" {
		return errors.New("--resume cannot be used with --name")
	}

	return nil
}

func runPromptWithContainer(
	cmd *cobra.Command,
	container *di.Container,
	options promptRunOptions,
	message string,
) error {
	runtime := container.AssistantService().Runtime

	cwd, err := assistant.DefaultCWD("")
	if err != nil {
		return cliError(err, cliResolveWorkingDirectory)
	}

	strategy := normalizedToolStrategy(options.ToolStrategy)
	metrics := new(assistant.RunMetrics)
	ctx := assistant.WithToolStrategy(cmd.Context(), strategy)
	ctx = assistant.WithRunMetrics(ctx, metrics)
	request := buildPromptRequest(cwd, message, options)
	request.OnEvent = metrics.ObserveStreamEvent
	started := time.Now()
	response, promptErr := runtime.Prompt(ctx, request)
	elapsed := time.Since(started)

	snapshot := metrics.Snapshot()
	measured := &promptMetrics{
		Strategy: string(strategy), Error: errorText(promptErr),
		ProviderRoundTrips: snapshot.ProviderRoundTrips, ElapsedMilliseconds: elapsed.Milliseconds(),
		InputTokens: snapshot.InputTokens, OutputTokens: snapshot.OutputTokens,
		ToolCalls: snapshot.ToolCalls, NestedToolCalls: snapshot.NestedToolCalls,
		TraceComplete: snapshot.TraceComplete, Success: promptErr == nil,
	}

	if err := promptExecutionError(promptErr, writePromptMetrics(options.MetricsJSON, measured)); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(cmd.OutOrStdout(), response.Text); err != nil {
		return oops.Wrapf(err, "write prompt response")
	}

	return nil
}

func promptExecutionError(promptErr, metricsErr error) error {
	if promptErr == nil {
		return metricsErr
	}

	promptErr = cliError(promptErr, "run prompt")
	if metricsErr != nil {
		return errors.Join(promptErr, metricsErr)
	}

	return promptErr
}

type promptMetrics struct {
	Strategy            string `json:"strategy"`
	Error               string `json:"error"`
	ProviderRoundTrips  int    `json:"provider_round_trips"`
	ElapsedMilliseconds int64  `json:"elapsed_ms"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	ToolCalls           int    `json:"tool_calls"`
	NestedToolCalls     int    `json:"nested_tool_calls"`
	TraceComplete       bool   `json:"trace_complete"`
	Success             bool   `json:"success"`
}

func normalizedToolStrategy(value string) assistant.ToolStrategy {
	strategy := assistant.ToolStrategy(value)
	if strategy == "" {
		return assistant.ToolStrategyHybrid
	}

	return strategy
}

func errorText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

func writePromptMetrics(path string, metrics *promptMetrics) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}

	encoded, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return oops.In("cli").Code("encode_prompt_metrics").Wrapf(err, "encode prompt metrics")
	}

	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, promptMetricsMode); err != nil {
		return oops.In("cli").Code("write_prompt_metrics").Wrapf(err, "write prompt metrics")
	}

	return nil
}

func buildPromptRequest(cwd, message string, options promptRunOptions) *assistant.PromptRequest {
	return &assistant.PromptRequest{
		OnEvent:        nil,
		OnRetry:        nil,
		OnUserEntry:    nil,
		ParentEntryID:  nil,
		SessionID:      options.SessionID,
		CWD:            cwd,
		Text:           message,
		Name:           options.SessionName,
		ResumeLatest:   options.Resume,
		HideUserPrompt: false,
	}
}

func promptMessage(cmd *cobra.Command, args []string) (string, error) {
	message := strings.TrimSpace(strings.Join(args, " "))
	if message != "" {
		return message, nil
	}

	stdin, err := limitio.ReadAll(cmd.InOrStdin(), promptStdinLimitBytes, "prompt stdin")
	if err != nil {
		return "", oops.Wrapf(err, "read stdin")
	}

	message = strings.TrimSpace(string(stdin))
	if message == "" {
		return "", errors.New("prompt message is required")
	}

	return message, nil
}
