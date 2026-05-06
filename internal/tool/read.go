package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/oops"
)

// ReadInput contains arguments for the read tool.
type ReadInput struct {
	Offset *int   `json:"offset,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
	Path   string `json:"path"`
}

// ReadTool reads text and image files from disk.
type ReadTool struct {
	cwd string
}

// NewReadTool creates the read tool for cwd.
func NewReadTool(cwd string) *ReadTool {
	return &ReadTool{cwd: cwd}
}

// Definition returns read tool metadata.
func (readTool *ReadTool) Definition() Definition {
	return Definition{
		Name:          NameRead,
		Label:         "read",
		Description:   readDescription(),
		PromptSnippet: "Read file contents",
		PromptGuidelines: []string{
			"Use read to examine files instead of cat or sed.",
		},
		ReadOnly: true,
	}
}

// Execute runs the read tool.
func (readTool *ReadTool) Execute(ctx context.Context, input map[string]any) (Result, error) {
	args, err := decodeInput[ReadInput](input)
	if err != nil {
		return emptyToolResult(), err
	}

	return readTool.Read(ctx, args)
}

// Read reads one text or supported image file.
func (readTool *ReadTool) Read(ctx context.Context, input ReadInput) (Result, error) {
	if err := validateReadInput(input); err != nil {
		return emptyToolResult(), err
	}
	absolutePath, err := ResolveReadPath(input.Path, readTool.cwd)
	if err != nil {
		return emptyToolResult(), oops.In("tool").Code("read_resolve_path").Wrapf(err, "resolve read path")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return emptyToolResult(), ctxErr
	}

	//nolint:gosec // The read tool intentionally reads user-selected workspace paths.
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return emptyToolResult(), oops.
			In("tool").
			Code("read_file").
			With("path", input.Path).
			Wrapf(err, "read file")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return emptyToolResult(), ctxErr
	}

	mimeType := detectSupportedImageMIMEType(absolutePath, data)
	if mimeType != "" {
		return imageReadResult(mimeType, data), nil
	}

	return textReadResult(input, string(data))
}

func validateReadInput(input ReadInput) error {
	if strings.TrimSpace(input.Path) == "" {
		return oops.In("tool").Code("read_path_required").Errorf("read path is required")
	}
	if input.Offset != nil && *input.Offset < 1 {
		return oops.In("tool").Code("read_invalid_offset").Errorf("read offset must be greater than zero")
	}
	if input.Limit != nil && *input.Limit < 1 {
		return oops.In("tool").Code("read_invalid_limit").Errorf("read limit must be greater than zero")
	}

	return nil
}

func readDescription() string {
	return fmt.Sprintf(
		"Read the contents of a file. Supports text files and images (jpg, png, gif, webp). "+
			"For text files, output is truncated to %d lines or %s (whichever is hit first). "+
			"Use offset/limit for large files.",
		DefaultMaxLines,
		FormatSize(DefaultMaxBytes),
	)
}

func imageReadResult(mimeType string, data []byte) Result {
	return Result{
		Content: []ContentBlock{
			{Type: ContentTypeText, Text: "Read image file [" + mimeType + "]", Data: "", MIMEType: ""},
			{Type: ContentTypeImage, Text: "", Data: base64.StdEncoding.EncodeToString(data), MIMEType: mimeType},
		},
		Details: map[string]any{},
	}
}

func textReadResult(input ReadInput, content string) (Result, error) {
	lines := strings.Split(content, "\n")
	startLine := 0
	if input.Offset != nil {
		startLine = *input.Offset - 1
	}
	if startLine >= len(lines) {
		return emptyToolResult(), oops.
			In("tool").
			Code("read_offset_beyond_eof").
			With("offset", input.Offset).
			With("total_lines", len(lines)).
			Errorf("read offset is beyond end of file")
	}

	selectedContent, userLimitedLines := selectReadContent(lines, startLine, input.Limit)
	truncation := TruncateHead(selectedContent, TruncationOptions{MaxLines: 0, MaxBytes: 0})
	outputText, details := formatReadOutput(input, lines, &truncation, startLine, userLimitedLines)

	return TextResult(outputText, details), nil
}

func selectReadContent(lines []string, startLine int, limit *int) (selectedContent string, userLimitedLines *int) {
	if limit == nil {
		return strings.Join(lines[startLine:], "\n"), nil
	}

	endLine := min(startLine+*limit, len(lines))
	selectedLines := endLine - startLine

	return strings.Join(lines[startLine:endLine], "\n"), &selectedLines
}

func formatReadOutput(
	input ReadInput,
	allLines []string,
	truncation *TruncationResult,
	startLine int,
	userLimitedLines *int,
) (output string, details map[string]any) {
	startLineDisplay := startLine + 1
	if truncation.FirstLineExceedsLimit {
		firstLineSize := FormatSize(len([]byte(allLines[startLine])))
		return fmt.Sprintf(
			"[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startLineDisplay,
			firstLineSize,
			FormatSize(DefaultMaxBytes),
			startLineDisplay,
			input.Path,
			DefaultMaxBytes,
		), map[string]any{detailTruncation: *truncation}
	}
	if truncation.Truncated {
		return truncatedReadOutput(truncation, startLineDisplay, len(allLines))
	}
	if userLimitedLines != nil && startLine+*userLimitedLines < len(allLines) {
		remainingLines := len(allLines) - (startLine + *userLimitedLines)
		nextOffset := startLine + *userLimitedLines + 1
		return fmt.Sprintf(
			"%s\n\n[%d more lines in file. Use offset=%d to continue.]",
			truncation.Content,
			remainingLines,
			nextOffset,
		), map[string]any{}
	}

	return truncation.Content, map[string]any{}
}

func truncatedReadOutput(
	truncation *TruncationResult,
	startLineDisplay int,
	totalFileLines int,
) (output string, details map[string]any) {
	endLineDisplay := startLineDisplay + truncation.OutputLines - 1
	nextOffset := endLineDisplay + 1
	if truncation.TruncatedBy == TruncatedByLines {
		return fmt.Sprintf(
			"%s\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]",
			truncation.Content,
			startLineDisplay,
			endLineDisplay,
			totalFileLines,
			nextOffset,
		), map[string]any{detailTruncation: *truncation}
	}

	return fmt.Sprintf(
		"%s\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
		truncation.Content,
		startLineDisplay,
		endLineDisplay,
		totalFileLines,
		FormatSize(DefaultMaxBytes),
		nextOffset,
	), map[string]any{detailTruncation: *truncation}
}

func detectSupportedImageMIMEType(filePath string, data []byte) string {
	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return mimeType
	default:
		return imageMIMETypeFromExtension(filePath)
	}
}

func imageMIMETypeFromExtension(filePath string) string {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}
