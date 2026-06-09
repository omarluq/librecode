package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	cwd string
}

type synchronizedBuffer struct {
	buffer bytes.Buffer
	lock   sync.Mutex
}

// NewBashTool creates the bash tool for cwd.
func NewBashTool(cwd string) *BashTool {
	return &BashTool{cwd: cwd}
}

// Definition returns bash tool metadata.
func (bashTool *BashTool) Definition() Definition {
	return Definition{
		Schema:        nil,
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
func (bashTool *BashTool) Execute(ctx context.Context, input map[string]any) (Result, error) {
	args, err := decodeInput[BashInput](input)
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
	execCtx, cancel := contextWithOptionalTimeout(ctx, input.Timeout)
	defer cancel()

	output, waitErr := runShellCommand(execCtx, workingDirectory, input.Command)

	return formatBashResult(execCtx, input, output.bytes(), waitErr)
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
	output := &commandOutput{buffer: &synchronizedBuffer{buffer: bytes.Buffer{}, lock: sync.Mutex{}}}
	shellPath, shellArgs, err := shellConfig(command)
	if err != nil {
		return output, err
	}

	//nolint:gosec // The bash tool intentionally executes model/user-supplied shell commands.
	cmd := exec.CommandContext(ctx, shellPath, shellArgs...)
	cmd.Dir = cwd
	configureShellCommand(cmd)
	cmd.Cancel = func() error {
		return terminateShellCommand(cmd)
	}
	cmd.WaitDelay = 2 * time.Second

	cmd.Stdout = output.buffer
	cmd.Stderr = output.buffer

	if err := cmd.Start(); err != nil {
		return output, err
	}

	return output, cmd.Wait()
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
	file, err := os.CreateTemp(outputDir, "librecode-bash-*.log")
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

	return outputPath, nil
}

func fullBashOutputDir() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", bashOutputFSError(err, "resolve cache dir for full bash output")
	}
	outputDir := filepath.Join(cacheDir, "librecode", "bash-output")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", bashOutputFSError(err, "create full bash output dir")
	}

	return outputDir, nil
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
	lastNewline := bytes.LastIndexByte([]byte(text), '\n')
	if lastNewline == -1 {
		return len([]byte(text))
	}

	return len([]byte(text[lastNewline+1:]))
}

func appendStatus(text, status string) string {
	if text == "" {
		return status
	}

	return text + "\n\n" + status
}

func (output *commandOutput) bytes() []byte {
	return output.buffer.bytes()
}

func (buffer *synchronizedBuffer) Write(data []byte) (int, error) {
	buffer.lock.Lock()
	defer buffer.lock.Unlock()

	return buffer.buffer.Write(data)
}

func (buffer *synchronizedBuffer) bytes() []byte {
	buffer.lock.Lock()
	defer buffer.lock.Unlock()

	return append([]byte{}, buffer.buffer.Bytes()...)
}
