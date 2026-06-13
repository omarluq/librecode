package tool

import (
	"fmt"
	"strconv"
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/samber/oops"
)

const (
	editDiffContextLines = 4
	editDiffMaxLines     = 400
	editDiffMaxBytes     = 24 * 1024
)

func generateDiffString(oldContent, newContent string) (EditDetails, error) {
	edits := udiff.Lines(oldContent, newContent)
	if len(edits) == 0 {
		return EditDetails{Diff: "", FirstChangedLine: 0, Truncated: false}, nil
	}

	diff, err := udiff.ToUnified("before", "after", oldContent, edits, editDiffContextLines)
	if err != nil {
		return EditDetails{Diff: "", FirstChangedLine: 0, Truncated: false}, oops.
			In("tool").
			Code("edit_generate_diff").
			Wrapf(err, "generate unified diff")
	}

	truncation := TruncateHead(diff, TruncationOptions{MaxLines: editDiffMaxLines, MaxBytes: editDiffMaxBytes})

	return EditDetails{
		Diff:             truncation.Content,
		FirstChangedLine: firstChangedLineFromUnifiedDiff(diff),
		Truncated:        truncation.Truncated,
	}, nil
}

func firstChangedLineFromUnifiedDiff(diff string) int {
	currentLine := 0

	for line := range strings.SplitSeq(diff, "\n") {
		if strings.HasPrefix(line, "@@") {
			lineNumber, matched := parseUnifiedHunkStart(line)
			if !matched {
				continue
			}

			currentLine = lineNumber

			continue
		}

		if currentLine == 0 || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "-"):
			return currentLine
		case strings.HasPrefix(line, "+"):
			return currentLine
		case strings.HasPrefix(line, " "):
			currentLine++
		}
	}

	return 0
}

func parseUnifiedHunkStart(header string) (int, bool) {
	parts := strings.Fields(header)
	if len(parts) < 3 || !strings.HasPrefix(parts[1], "-") || !strings.HasPrefix(parts[2], "+") {
		return 0, false
	}

	oldStart, oldLength, matched := parseUnifiedRange(strings.TrimPrefix(parts[1], "-"))
	if !matched {
		return 0, false
	}

	newStart, _, matched := parseUnifiedRange(strings.TrimPrefix(parts[2], "+"))
	if !matched {
		return 0, false
	}

	if oldLength == 0 {
		return normalizeUnifiedLineNumber(newStart), true
	}

	return normalizeUnifiedLineNumber(oldStart), true
}

func parseUnifiedRange(lineRange string) (start, length int, ok bool) {
	lineText, lengthText, hasLength := strings.Cut(lineRange, ",")

	lineNumber, err := strconv.Atoi(lineText)
	if err != nil {
		return 0, 0, false
	}

	if !hasLength {
		return lineNumber, 1, true
	}

	lineCount, err := strconv.Atoi(lengthText)
	if err != nil {
		return 0, 0, false
	}

	return lineNumber, lineCount, true
}

func normalizeUnifiedLineNumber(lineNumber int) int {
	if lineNumber == 0 {
		return 1
	}

	return lineNumber
}

func diffTruncationMessage(details EditDetails) string {
	if !details.Truncated {
		return ""
	}

	return fmt.Sprintf("\n\n[diff truncated to %d lines / %s]", editDiffMaxLines, FormatSize(editDiffMaxBytes))
}
