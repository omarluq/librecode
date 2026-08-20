package tool

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/samber/oops"
)

// BashInput contains arguments for the bash tool.
type BashInput struct {
	Timeout *float64 `json:"timeout,omitempty"`
	Command string   `json:"command"`
}

// BashTool executes shell commands in the configured working directory.
type BashTool struct {
	locks *fileMutationLocks
	cwd   string
}

type synchronizedBuffer struct {
	buffer    []byte
	total     int64
	truncated bool
	lock      sync.Mutex
}

// NewBashTool creates the bash tool for cwd.
func NewBashTool(cwd string) *BashTool { return newBashTool(cwd, newFileMutationLocks()) }

func newBashTool(cwd string, locks *fileMutationLocks) *BashTool {
	return &BashTool{locks: locks, cwd: cwd}
}

// Definition returns bash tool metadata.
func (bashTool *BashTool) Definition() Definition {
	return Definition{
		Schema:        inputSchemaForName(NameBash),
		Name:          NameBash,
		Label:         "bash",
		Description:   bashDescription(),
		PromptSnippet: "Execute bash commands (ls, grep, find, etc.)",
		PromptGuidelines: []string{
			"Use bash for file operations like ls, rg, find.",
		},
		ReadOnly: false,
	}
}

// Execute runs the bash tool.
func (bashTool *BashTool) Execute(ctx context.Context, input Arguments) (Result, error) {
	var args BashInput

	err := decodeInput(input, &args)
	if err != nil {
		return emptyToolResult(), err
	}

	return bashTool.Bash(ctx, args)
}

// Bash executes a command and returns combined stdout and stderr.
func (bashTool *BashTool) Bash(ctx context.Context, input BashInput) (Result, error) {
	if strings.TrimSpace(input.Command) == "" {
		return emptyToolResult(), oops.In("tool").Code("bash_command_required").Errorf("bash command is required")
	}

	workingDirectory, err := bashTool.workingDirectory()
	if err != nil {
		return emptyToolResult(), err
	}

	return bashTool.locks.mutate(ctx, workingDirectory, func() (Result, error) {
		return bashTool.run(ctx, workingDirectory, input)
	})
}

func (bashTool *BashTool) prepareExecution(input Arguments) (preparedExecution, error) {
	var args BashInput
	if err := decodeInput(input, &args); err != nil {
		return preparedExecution{}, err
	}

	if strings.TrimSpace(args.Command) == "" {
		return preparedExecution{}, oops.In("tool").Code("bash_command_required").Errorf("bash command is required")
	}

	workingDirectory, err := bashTool.workingDirectory()
	if err != nil {
		return preparedExecution{}, err
	}

	return preparedExecution{
		mutationLocks: bashTool.locks,
		mutationPath:  workingDirectory,
		execute: func(ctx context.Context) (Result, error) {
			return bashTool.run(ctx, workingDirectory, args)
		},
	}, nil
}

func (*BashTool) run(ctx context.Context, workingDirectory string, input BashInput) (Result, error) {
	execCtx, cancel := contextWithOptionalTimeout(ctx, input.Timeout)
	defer cancel()

	output, waitErr := runShellCommand(execCtx, workingDirectory, input.Command)
	captured, totalBytes, ingestionTruncated := output.snapshot()

	result, resultErr := formatBashResult(execCtx, input, captured, waitErr)
	if !ingestionTruncated {
		return result, resultErr
	}

	metadata := map[string]any{
		"truncated": true, "truncated_by": "ingestion_bytes",
		"total_bytes": totalBytes, "retained_bytes": len(captured),
	}

	if result.Details == nil {
		result.Details = map[string]any{}
	}

	result.Details[detailTruncation] = metadata

	notice := fmt.Sprintf(
		"[Output ingestion truncated: retained last %s of %s]",
		FormatSize(len(captured)), FormatSize(int(totalBytes)),
	)
	if resultErr != nil {
		return result, errors.New(appendStatus(resultErr.Error(), notice))
	}

	if len(result.Content) > 0 {
		result.Content[0].Text = appendStatus(result.Content[0].Text, notice)
	}

	return result, nil
}

func (bashTool *BashTool) workingDirectory() (string, error) {
	workingDirectory, err := ResolveToCWD(".", bashTool.cwd)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(workingDirectory)
	if err != nil {
		return "", fmt.Errorf("working directory does not exist: %s", workingDirectory)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", workingDirectory)
	}

	return workingDirectory, nil
}

func bashDescription() string {
	return fmt.Sprintf(
		"Execute a bash command in the current working directory. Returns stdout and stderr. "+
			"Output is truncated to last %d lines or %s. Optionally provide timeout in seconds.",
		DefaultMaxLines,
		FormatSize(DefaultMaxBytes),
	)
}

func contextWithOptionalTimeout(parent context.Context, timeout *float64) (context.Context, context.CancelFunc) {
	if timeout == nil || *timeout <= 0 {
		return context.WithCancel(parent)
	}

	return context.WithTimeout(parent, time.Duration(*timeout*float64(time.Second)))
}

type commandOutput struct {
	buffer *synchronizedBuffer
}

func runShellCommand(ctx context.Context, cwd, command string) (*commandOutput, error) {
	output := &commandOutput{buffer: &synchronizedBuffer{
		buffer: make([]byte, 0, DefaultMaxBytes), total: 0, truncated: false, lock: sync.Mutex{},
	}}

	shellPath, shellArgs := shellConfig(command)

	cmd := shellCommandContext(ctx, shellPath, shellArgs)
	cmd.Dir = cwd
	configureShellCommand(cmd)
	cmd.Cancel = func() error {
		return terminateShellCommand(cmd)
	}
	cmd.WaitDelay = commandWaitDelay

	cmd.Stdout = output.buffer
	cmd.Stderr = output.buffer

	if err := cmd.Start(); err != nil {
		return output, toolWrap(err, "start bash command")
	}

	return output, toolWrap(cmd.Wait(), "wait for bash command")
}

func formatBashResult(ctx context.Context, input BashInput, output []byte, waitErr error) (Result, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		outputText, _, err := formatBashOutput(output, "")
		if err != nil {
			return emptyToolResult(), err
		}

		if errors.Is(contextErr, context.DeadlineExceeded) && input.Timeout != nil {
			status := fmt.Sprintf("Command timed out after %.3g seconds", *input.Timeout)

			return emptyToolResult(), errors.New(appendStatus(outputText, status))
		}

		return emptyToolResult(), errors.New(appendStatus(outputText, "Command aborted"))
	}

	if waitErr != nil {
		return formatBashWaitError(output, waitErr)
	}

	outputText, details, err := formatBashOutput(output, "(no output)")
	if err != nil {
		return emptyToolResult(), err
	}

	return TextResult(outputText, details), nil
}

func formatBashWaitError(output []byte, waitErr error) (Result, error) {
	outputText, _, err := formatBashOutput(output, "(no output)")
	if err != nil {
		return emptyToolResult(), err
	}

	if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
		status := fmt.Sprintf("Command exited with code %d", exitErr.ExitCode())

		return emptyToolResult(), errors.New(appendStatus(outputText, status))
	}

	return emptyToolResult(), waitErr
}

func formatBashOutput(output []byte, emptyText string) (outputText string, details map[string]any, err error) {
	text := string(output)
	truncation := TruncateTail(text, TruncationOptions{MaxLines: 0, MaxBytes: 0})

	outputText = truncation.Content
	if outputText == "" {
		outputText = emptyText
	}

	if !truncation.Truncated {
		return outputText, map[string]any{}, nil
	}

	fullOutputPath, err := writeFullBashOutput(output)
	if err != nil {
		return "", map[string]any{}, err
	}

	notice := bashTruncationNotice(&truncation, fullOutputPath, lastLineByteCount(text))

	return outputText + "\n\n" + notice, map[string]any{
		detailTruncation:     truncation,
		detailFullOutputPath: fullOutputPath,
	}, nil
}

func bashTruncationNotice(truncation *TruncationResult, fullOutputPath string, lastLineBytes int) string {
	startLine := truncation.TotalLines - truncation.OutputLines + 1

	endLine := truncation.TotalLines
	if truncation.LastLinePartial {
		return fmt.Sprintf(
			"[Showing last %s of line %d (line is %s). Full output: %s]",
			FormatSize(truncation.OutputBytes),
			endLine,
			FormatSize(lastLineBytes),
			fullOutputPath,
		)
	}

	if truncation.TruncatedBy == TruncatedByLines {
		return fmt.Sprintf("[Showing lines %d-%d of %d. Full output: %s]", startLine, endLine, endLine, fullOutputPath)
	}

	return fmt.Sprintf(
		"[Showing lines %d-%d of %d (%s limit). Full output: %s]",
		startLine,
		endLine,
		endLine,
		FormatSize(DefaultMaxBytes),
		fullOutputPath,
	)
}

func writeFullBashOutput(output []byte) (string, error) {
	outputDir, err := fullBashOutputDir()
	if err != nil {
		return "", err
	}

	file, err := os.CreateTemp(outputDir, fullBashOutputPrefix+"*.log")
	if err != nil {
		return "", bashOutputFSError(err, "create full bash output file")
	}

	outputPath := file.Name()
	if _, err := file.Write(output); err != nil {
		cleanupErr := errors.Join(
			bashOutputCleanupError(file.Close(), "close full bash output"),
			bashOutputCleanupError(os.Remove(outputPath), "remove full bash output file"),
		)

		return "", errors.Join(bashOutputFSError(err, "write full bash output"), cleanupErr)
	}

	if err := file.Close(); err != nil {
		cleanupErr := bashOutputCleanupError(os.Remove(outputPath), "remove full bash output file")

		return "", errors.Join(bashOutputFSError(err, "close full bash output"), cleanupErr)
	}

	cleanupStaleBashOutputs(outputDir, outputPath, time.Now())

	return outputPath, nil
}

func fullBashOutputDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", bashOutputFSError(err, "resolve cache dir for full bash output")
	}

	outputDir := filepath.Join(cacheDir, "librecode", "bash-output")
	if err := os.MkdirAll(outputDir, secureDirMode); err != nil {
		return "", bashOutputFSError(err, "create full bash output dir")
	}

	return outputDir, nil
}

// cleanupStaleBashOutputs removes full bash output logs older than the retention
// threshold. Cleanup is opportunistic: the freshly written output is always kept
// and any failure is logged instead of failing the tool call that triggered it.
func cleanupStaleBashOutputs(outputDir, keepPath string, now time.Time) {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("read full bash output dir for cleanup", "dir", outputDir, "error", err)
		}

		return
	}

	cutoff := now.Add(-fullBashOutputRetention)

	for _, entry := range entries {
		removeStaleBashOutput(entry, outputDir, keepPath, cutoff)
	}
}

// removeStaleBashOutput removes a single stale full-output log unless it is the
// freshly written file or newer than the retention cutoff.
func removeStaleBashOutput(entry os.DirEntry, outputDir, keepPath string, cutoff time.Time) {
	if entry.IsDir() || !strings.HasPrefix(entry.Name(), fullBashOutputPrefix) {
		return
	}

	outputPath := filepath.Join(outputDir, entry.Name())
	if keepPath != "" && outputPath == keepPath {
		return
	}

	info, err := entry.Info()
	if err != nil {
		slog.Debug("stat full bash output for cleanup", "path", outputPath, "error", err)

		return
	}

	if info.ModTime().After(cutoff) {
		return
	}

	if err := os.Remove(outputPath); err != nil {
		slog.Debug("remove stale full bash output", "path", outputPath, "error", err)
	}
}

func bashOutputFSError(err error, message string) error {
	return oops.In("tool.bash").Code("bash-output-fs").Wrapf(err, "%s", message)
}

func bashOutputCleanupError(err error, message string) error {
	if err == nil {
		return nil
	}

	return bashOutputFSError(err, message)
}

func lastLineByteCount(text string) int {
	lastNewline := strings.LastIndexByte(text, '\n')
	if lastNewline == -1 {
		return len(text)
	}

	return len(text[lastNewline+1:])
}

func appendStatus(text, status string) string {
	if text == "" {
		return status
	}

	return text + "\n\n" + status
}

func (output *commandOutput) snapshot() (captured []byte, total int64, truncated bool) {
	return output.buffer.snapshot()
}

func (buffer *synchronizedBuffer) Write(data []byte) (int, error) {
	buffer.lock.Lock()
	defer buffer.lock.Unlock()

	buffer.total += int64(len(data))
	if len(data) >= DefaultMaxBytes {
		buffer.buffer = append(buffer.buffer[:0], data[len(data)-DefaultMaxBytes:]...)
		buffer.truncated = len(data) > DefaultMaxBytes || buffer.total > int64(len(data))

		return len(data), nil
	}

	if overflow := len(buffer.buffer) + len(data) - DefaultMaxBytes; overflow > 0 {
		copy(buffer.buffer, buffer.buffer[overflow:])
		buffer.buffer = buffer.buffer[:len(buffer.buffer)-overflow]
		buffer.truncated = true
	}

	buffer.buffer = append(buffer.buffer, data...)

	return len(data), nil
}

func (buffer *synchronizedBuffer) snapshot() (captured []byte, total int64, truncated bool) {
	buffer.lock.Lock()
	defer buffer.lock.Unlock()

	return append([]byte{}, buffer.buffer...), buffer.total, buffer.truncated
}
