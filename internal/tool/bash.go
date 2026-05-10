package tool

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
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
	workingDirectory, err := bashTool.workingDirectory()
	if err != nil {
		return emptyToolResult(), err
	}
	execCtx, cancel := contextWithOptionalTimeout(ctx, input.Timeout)
	defer cancel()

	output, waitErr := runShellCommand(execCtx, workingDirectory, input.Command)
	if copyErr := output.copyError(); copyErr != nil {
		return emptyToolResult(), copyErr
	}

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
	errs   []error
}

func runShellCommand(ctx context.Context, cwd, command string) (*commandOutput, error) {
	output := &commandOutput{buffer: &synchronizedBuffer{buffer: bytes.Buffer{}, lock: sync.Mutex{}}, errs: []error{}}
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

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return output, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return output, err
	}
	if err := cmd.Start(); err != nil {
		return output, err
	}

	copyErrs := copyCommandOutput(output.buffer, stdout, stderr)
	waitErr := cmd.Wait()
	output.errs = copyErrs()

	return output, waitErr
}

func copyCommandOutput(output io.Writer, stdout, stderr io.Reader) func() []error {
	copyErrs := make(chan error, 2)
	copyStream := func(reader io.Reader) {
		_, err := io.Copy(output, reader)
		if err != nil {
			copyErrs <- err
			return
		}
		copyErrs <- nil
	}
	go copyStream(stdout)
	go copyStream(stderr)

	return func() []error {
		errs := make([]error, 0, 2)
		for range 2 {
			if err := <-copyErrs; err != nil {
				errs = append(errs, err)
			}
		}

		return errs
	}
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
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
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
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate temp output path: %w", err)
	}
	outputPath := filepath.Join(os.TempDir(), "librecode-bash-"+hex.EncodeToString(randomBytes)+".log")
	if err := os.WriteFile(outputPath, output, 0o600); err != nil {
		return "", fmt.Errorf("write full bash output: %w", err)
	}

	return outputPath, nil
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

func (output *commandOutput) copyError() error {
	return errors.Join(output.errs...)
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
