package tool

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMaxLines is the default line limit for tool outputs.
	DefaultMaxLines = 2000
	// DefaultMaxBytes is the default byte limit for tool outputs.
	DefaultMaxBytes = 50 * 1024
	// GrepMaxLineLength is the maximum displayed length for one grep match line.
	GrepMaxLineLength = 500
)

// TruncatedBy identifies the limit that caused truncation.
type TruncatedBy string

const (
	// TruncatedByNone means content was not truncated.
	TruncatedByNone TruncatedBy = ""
	// TruncatedByLines means the line limit was reached first.
	TruncatedByLines TruncatedBy = "lines"
	// TruncatedByBytes means the byte limit was reached first.
	TruncatedByBytes TruncatedBy = "bytes"
)

// TruncationOptions controls head or tail truncation.
type TruncationOptions struct {
	MaxLines int `json:"max_lines"`
	MaxBytes int `json:"max_bytes"`
}

// TruncationResult describes how content was truncated.
type TruncationResult struct {
	TruncatedBy           TruncatedBy `json:"truncated_by"`
	Content               string      `json:"content"`
	TotalLines            int         `json:"total_lines"`
	TotalBytes            int         `json:"total_bytes"`
	OutputLines           int         `json:"output_lines"`
	OutputBytes           int         `json:"output_bytes"`
	MaxLines              int         `json:"max_lines"`
	MaxBytes              int         `json:"max_bytes"`
	Truncated             bool        `json:"truncated"`
	LastLinePartial       bool        `json:"last_line_partial"`
	FirstLineExceedsLimit bool        `json:"first_line_exceeds_limit"`
}

// FormatSize formats bytes for user-facing truncation notices.
func FormatSize(byteCount int) string {
	if byteCount < 1024 {
		return fmt.Sprintf("%dB", byteCount)
	}
	if byteCount < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(byteCount)/1024)
	}

	return fmt.Sprintf("%.1fMB", float64(byteCount)/(1024*1024))
}

// TruncateHead keeps the first complete lines that fit within both limits.
func TruncateHead(content string, options TruncationOptions) TruncationResult {
	limits := normalizeTruncationOptions(options)
	lines := strings.Split(content, "\n")
	totalBytes := len([]byte(content))
	if len(lines) <= limits.MaxLines && totalBytes <= limits.MaxBytes {
		return newTruncationResult(content, false, TruncatedByNone, false, false, lines, limits)
	}
	if len([]byte(lines[0])) > limits.MaxBytes {
		return TruncationResult{
			Truncated:             true,
			LastLinePartial:       false,
			FirstLineExceedsLimit: true,
			TruncatedBy:           TruncatedByBytes,
			TotalLines:            len(lines),
			TotalBytes:            totalBytes,
			OutputLines:           0,
			OutputBytes:           0,
			MaxLines:              limits.MaxLines,
			MaxBytes:              limits.MaxBytes,
			Content:               "",
		}
	}

	outputLines, truncatedBy := collectHeadLines(lines, limits)
	outputContent := strings.Join(outputLines, "\n")

	return TruncationResult{
		Truncated:             true,
		LastLinePartial:       false,
		FirstLineExceedsLimit: false,
		TruncatedBy:           truncatedBy,
		TotalLines:            len(lines),
		TotalBytes:            totalBytes,
		OutputLines:           len(outputLines),
		OutputBytes:           len([]byte(outputContent)),
		MaxLines:              limits.MaxLines,
		MaxBytes:              limits.MaxBytes,
		Content:               outputContent,
	}
}

// TruncateTail keeps the last complete lines that fit within both limits.
func TruncateTail(content string, options TruncationOptions) TruncationResult {
	limits := normalizeTruncationOptions(options)
	lines := strings.Split(content, "\n")
	totalBytes := len([]byte(content))
	if len(lines) <= limits.MaxLines && totalBytes <= limits.MaxBytes {
		return newTruncationResult(content, false, TruncatedByNone, false, false, lines, limits)
	}

	outputLines, truncatedBy, lastLinePartial := collectTailLines(lines, limits)
	outputContent := strings.Join(outputLines, "\n")

	return TruncationResult{
		Truncated:             true,
		LastLinePartial:       lastLinePartial,
		FirstLineExceedsLimit: false,
		TruncatedBy:           truncatedBy,
		TotalLines:            len(lines),
		TotalBytes:            totalBytes,
		OutputLines:           len(outputLines),
		OutputBytes:           len([]byte(outputContent)),
		MaxLines:              limits.MaxLines,
		MaxBytes:              limits.MaxBytes,
		Content:               outputContent,
	}
}

// TruncateLine limits one display line to maxCharacters runes.
func TruncateLine(line string, maxCharacters int) (text string, wasTruncated bool) {
	if maxCharacters <= 0 {
		maxCharacters = GrepMaxLineLength
	}
	runes := []rune(line)
	if len(runes) <= maxCharacters {
		return line, false
	}

	return string(runes[:maxCharacters]) + "... [truncated]", true
}

func normalizeTruncationOptions(options TruncationOptions) TruncationOptions {
	limits := options
	if limits.MaxLines <= 0 {
		limits.MaxLines = DefaultMaxLines
	}
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = DefaultMaxBytes
	}

	return limits
}

func collectHeadLines(lines []string, limits TruncationOptions) ([]string, TruncatedBy) {
	outputLines := make([]string, 0, min(len(lines), limits.MaxLines))
	outputBytes := 0
	truncatedBy := TruncatedByLines
	for lineIndex := 0; lineIndex < len(lines) && lineIndex < limits.MaxLines; lineIndex++ {
		lineBytes := len([]byte(lines[lineIndex]))
		if lineIndex > 0 {
			lineBytes++
		}
		if outputBytes+lineBytes > limits.MaxBytes {
			truncatedBy = TruncatedByBytes
			break
		}
		outputLines = append(outputLines, lines[lineIndex])
		outputBytes += lineBytes
	}

	return outputLines, truncatedBy
}

func collectTailLines(lines []string, limits TruncationOptions) ([]string, TruncatedBy, bool) {
	outputLines := make([]string, 0, min(len(lines), limits.MaxLines))
	outputBytes := 0
	truncatedBy := TruncatedByLines
	lastLinePartial := false
	for lineIndex := len(lines) - 1; lineIndex >= 0 && len(outputLines) < limits.MaxLines; lineIndex-- {
		lineBytes := len([]byte(lines[lineIndex]))
		if len(outputLines) > 0 {
			lineBytes++
		}
		if outputBytes+lineBytes > limits.MaxBytes {
			truncatedBy = TruncatedByBytes
			if len(outputLines) == 0 {
				outputLines = append(outputLines, truncateStringFromEnd(lines[lineIndex], limits.MaxBytes))
				lastLinePartial = true
			}
			break
		}
		outputLines = append(outputLines, lines[lineIndex])
		outputBytes += lineBytes
	}
	reverseStrings(outputLines)

	return outputLines, truncatedBy, lastLinePartial
}

func newTruncationResult(
	content string,
	truncated bool,
	truncatedBy TruncatedBy,
	lastLinePartial bool,
	firstLineExceedsLimit bool,
	lines []string,
	limits TruncationOptions,
) TruncationResult {
	return TruncationResult{
		Truncated:             truncated,
		LastLinePartial:       lastLinePartial,
		FirstLineExceedsLimit: firstLineExceedsLimit,
		TruncatedBy:           truncatedBy,
		TotalLines:            len(lines),
		TotalBytes:            len([]byte(content)),
		OutputLines:           len(lines),
		OutputBytes:           len([]byte(content)),
		MaxLines:              limits.MaxLines,
		MaxBytes:              limits.MaxBytes,
		Content:               content,
	}
}

func truncateStringFromEnd(value string, maxBytes int) string {
	data := []byte(value)
	if len(data) <= maxBytes {
		return value
	}

	start := len(data) - maxBytes
	for start < len(data) && !utf8.RuneStart(data[start]) {
		start++
	}

	return string(data[start:])
}

func reverseStrings(values []string) {
	for leftIndex, rightIndex := 0, len(values)-1; leftIndex < rightIndex; {
		values[leftIndex], values[rightIndex] = values[rightIndex], values[leftIndex]
		leftIndex++
		rightIndex--
	}
}
